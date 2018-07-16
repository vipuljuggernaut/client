package attachments

import (
	"bufio"
	"errors"
	"io"
	"net/http"
	"os"

	"github.com/keybase/client/go/chat/globals"
	"github.com/keybase/client/go/chat/s3"
	"github.com/keybase/client/go/chat/storage"
	"github.com/keybase/client/go/chat/utils"
	"github.com/keybase/client/go/libkb"
	"github.com/keybase/client/go/protocol/chat1"
	"github.com/keybase/client/go/protocol/gregor1"
	"golang.org/x/net/context"
	"golang.org/x/sync/errgroup"
)

type fileReadResetter struct {
	filename string
	file     *os.File
	buf      *bufio.Reader
}

func newFileReadResetter(name string) (*fileReadResetter, error) {
	f := &fileReadResetter{filename: name}
	if err := f.open(); err != nil {
		return nil, err
	}
	return f, nil
}

func (f *fileReadResetter) open() error {
	ff, err := os.Open(f.filename)
	if err != nil {
		return err
	}
	f.file = ff
	f.buf = bufio.NewReader(f.file)
	return nil
}

func (f *fileReadResetter) Read(p []byte) (int, error) {
	return f.buf.Read(p)
}

func (f *fileReadResetter) Reset() error {
	_, err := f.file.Seek(0, io.SeekStart)
	if err != nil {
		return err
	}
	f.buf.Reset(f.file)
	return nil
}

func (f *fileReadResetter) Close() error {
	f.buf = nil
	if f.file != nil {
		return f.file.Close()
	}
	return nil
}

type uploaderJob struct {
	OutboxID chat1.OutboxID
}

type Uploader struct {
	globals.Contextified
	utils.DebugLabeler

	store    *Store
	ri       func() chat1.RemoteInterface
	chatUI   func(sessionID int) libkb.ChatUI
	s3signer s3.Signer
}

func NewUploader(g *globals.Context, store *Store, s3signer s3.Signer,
	ri func() chat1.RemoteInterface,
	chatUI func(sessionID int) libkb.ChatUI) *Uploader {
	return &Uploader{
		Contextified: globals.NewContextified(g),
		DebugLabeler: utils.NewDebugLabeler(g.GetLog(), "Attachments.Uploader", false),
		store:        store,
		ri:           ri,
		chatUI:       chatUI,
		s3signer:     s3signer,
	}
}

func (u *Uploader) dbKey(outboxID chat1.OutboxID) libkb.DbKey {
	return libkb.DbKey{
		Typ: libkb.DBAttachmentUploader,
		Key: outboxID.String(),
	}
}

func (u *Uploader) Complete(ctx context.Context, outboxID chat1.OutboxID) error {
	return errors.New("not implemented")
}

func (u *Uploader) Retry(ctx context.Context, outboxID chat1.OutboxID) error {
	return errors.New("not implemented")
}

type UploadRegisterRes struct {
	Error      error
	Object     chat1.Asset
	Preview    *chat1.Asset
	Preprocess Preprocess
}

func (u *Uploader) Register(ctx context.Context, uid gregor1.UID, convID chat1.ConversationID,
	outboxID chat1.OutboxID, title, filename string) (chan UploadRegisterRes, error) {
	return u.upload(ctx, uid, convID, outboxID, title, filename)
}

func (u *Uploader) upload(ctx context.Context, uid gregor1.UID, convID chat1.ConversationID,
	outboxID chat1.OutboxID, title, filename string) (res chan UploadRegisterRes, err error) {
	// Stat the file to get size
	finfo, err := os.Stat(filename)
	if err != nil {
		return res, err
	}
	src, err := newFileReadResetter(filename)
	if err != nil {
		return res, err
	}
	// get s3 upload params from server
	params, err := u.ri().GetS3Params(ctx, convID)
	if err != nil {
		return res, err
	}
	progress := func(bytesComplete, bytesTotal int64) {
		parg := chat1.ChatAttachmentUploadProgressArg{
			BytesComplete: bytesComplete,
			BytesTotal:    bytesTotal,
		}
		u.chatUI(0).ChatAttachmentUploadProgress(ctx, parg)
	}

	// preprocess asset (get content type, create preview if possible) arg.SessionID,
	var ures UploadRegisterRes
	ures.Preprocess, err = u.PreprocessAsset(ctx, filename)
	if err != nil {
		return res, err
	}
	if ures.Preprocess.Preview != nil {
		u.Debug(ctx, "upload: created preview in preprocess")
		// Store the preview in pending storage
		if err := storage.NewPendingPreviews(u.G()).Put(ctx, outboxID, ures.Preprocess.Preview); err != nil {
			return res, err
		}
	}

	// upload attachment and (optional) preview concurrently
	var g errgroup.Group
	u.Debug(ctx, "upload: uploading assets")
	g.Go(func() error {
		u.chatUI(0).ChatAttachmentUploadStart(ctx, ures.Preprocess.BaseMetadata(), 0)
		var err error
		task := UploadTask{
			S3Params:       params,
			Filename:       filename,
			FileSize:       int(finfo.Size()),
			Plaintext:      src,
			S3Signer:       u.s3signer,
			ConversationID: convID,
			UserID:         uid,
			Progress:       progress,
		}
		ures.Object, err = u.store.UploadAsset(ctx, &task)
		//chatUI.ChatAttachmentUploadDone(ctx)
		if err != nil {
			u.Debug(ctx, "upload: error uploading primary asset to s3: %s", err)
		} else {
			ures.Object.Title = title
			ures.Object.MimeType = ures.Preprocess.ContentType
			ures.Object.Metadata = ures.Preprocess.BaseMetadata()
		}
		return err
	})

	if ures.Preprocess.Preview != nil {
		g.Go(func() error {
			u.chatUI(0).ChatAttachmentPreviewUploadStart(ctx, ures.Preprocess.PreviewMetadata())
			// copy the params so as not to mess with the main params above
			previewParams := params

			// add preview suffix to object key (P in hex)
			// the s3path in gregor is expecting hex here
			previewParams.ObjectKey += "50"
			task := UploadTask{
				S3Params:       previewParams,
				Filename:       filename,
				FileSize:       len(ures.Preprocess.Preview),
				Plaintext:      newBufReadResetter(ures.Preprocess.Preview),
				S3Signer:       u.s3signer,
				ConversationID: convID,
				UserID:         uid,
				Progress:       progress,
			}
			preview, err := u.store.UploadAsset(ctx, &task)
			u.chatUI(0).ChatAttachmentPreviewUploadDone(ctx)
			if err == nil {
				ures.Preview = &preview
				ures.Preview.MimeType = ures.Preprocess.ContentType
				ures.Preview.Metadata = ures.Preprocess.PreviewMetadata()
				ures.Preview.Tag = chat1.AssetTag_PRIMARY
			} else {
				u.Debug(ctx, "upload: error uploading preview asset to s3: %s", err)
			}
			return err
		})
	} else {
		g.Go(func() error {
			u.chatUI(0).ChatAttachmentPreviewUploadStart(ctx, chat1.AssetMetadata{})
			u.chatUI(0).ChatAttachmentPreviewUploadDone(ctx)
			return nil
		})
	}

	res = make(chan UploadRegisterRes, 1)
	go func() {
		if err := g.Wait(); err != nil {
			ures.Error = err
		}
		res <- ures
	}()
	return res, nil
}

func (u *Uploader) PreprocessAsset(ctx context.Context, filename string) (p Preprocess, err error) {
	src, err := os.Open(filename)
	if err != nil {
		return p, err
	}
	defer src.Close()

	head := make([]byte, 512)
	_, err = io.ReadFull(src, head)
	if err != nil && err != io.ErrUnexpectedEOF {
		return p, err
	}

	p = Preprocess{
		ContentType: http.DetectContentType(head),
	}
	u.Debug(ctx, "preprocessAsset: detected attachment content type %s", p.ContentType)
	if _, err := src.Seek(0, 0); err != nil {
		return p, err
	}
	previewRes, err := Preview(ctx, u.G().Log, src, p.ContentType, filename)
	if err != nil {
		u.Debug(ctx, "preprocessAsset: error making preview: %s", err)
		return p, err
	}
	if previewRes != nil {
		u.Debug(ctx, "preprocessAsset: made preview for attachment asset")
		p.Preview = previewRes.Source
		p.PreviewContentType = previewRes.ContentType
		if previewRes.BaseWidth > 0 || previewRes.BaseHeight > 0 {
			p.BaseDim = &Dimension{Width: previewRes.BaseWidth, Height: previewRes.BaseHeight}
		}
		if previewRes.PreviewWidth > 0 || previewRes.PreviewHeight > 0 {
			p.PreviewDim = &Dimension{Width: previewRes.PreviewWidth, Height: previewRes.PreviewHeight}
		}
		p.BaseDurationMs = previewRes.BaseDurationMs
		p.PreviewDurationMs = previewRes.PreviewDurationMs
	}

	return p, nil
}
