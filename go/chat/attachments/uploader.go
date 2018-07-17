package attachments

import (
	"bufio"
	"errors"
	"io"
	"os"

	"github.com/keybase/client/go/chat/globals"
	"github.com/keybase/client/go/chat/s3"
	"github.com/keybase/client/go/chat/storage"
	"github.com/keybase/client/go/chat/types"
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

type Uploader struct {
	globals.Contextified
	utils.DebugLabeler

	store    *Store
	ri       func() chat1.RemoteInterface
	s3signer s3.Signer
}

var _ types.AttachmentUploader = (*Uploader)(nil)

func NewUploader(g *globals.Context, store *Store, s3signer s3.Signer, ri func() chat1.RemoteInterface) *Uploader {
	return &Uploader{
		Contextified: globals.NewContextified(g),
		DebugLabeler: utils.NewDebugLabeler(g.GetLog(), "Attachments.Uploader", false),
		store:        store,
		ri:           ri,
		s3signer:     s3signer,
	}
}

func (u *Uploader) Store() *Store {
	return u.store
}

func (u *Uploader) dbKey(outboxID chat1.OutboxID) libkb.DbKey {
	return libkb.DbKey{
		Typ: libkb.DBAttachmentUploader,
		Key: outboxID.String(),
	}
}

func (u *Uploader) Retry(ctx context.Context, outboxID chat1.OutboxID) error {
	return errors.New("not implemented")
}

func (u *Uploader) Status(ctx context.Context, outboxID chat1.OutboxID) (res types.AttachmentUploadStatus, err error) {
	key := u.dbKey(outboxID)
	found, err := u.G().GetKVStore().GetInto(&res, key)
	if err != nil {
		return res, err
	}
	if !found {
		return res, libkb.NotFoundError{Msg: "no upload with given outbox ID"}
	}
	return res, nil
}

func (u *Uploader) setStatus(ctx context.Context, outboxID chat1.OutboxID,
	status types.AttachmentUploadStatus) error {
	key := u.dbKey(outboxID)
	return u.G().GetKVStore().PutObj(key, nil, status)
}

func (u *Uploader) Register(ctx context.Context, uid gregor1.UID, convID chat1.ConversationID,
	outboxID chat1.OutboxID, title, filename string, metadata []byte,
	chatUI func(sessionID int) libkb.ChatUI) (chan types.AttachmentUploadStatus, error) {
	var status types.AttachmentUploadStatus
	status.Status = types.AttachmentUploaderTaskStatusUploading
	if err := u.setStatus(ctx, outboxID, status); err != nil {
		return nil, err
	}
	return u.upload(ctx, uid, convID, outboxID, title, filename, metadata, chatUI)
}

func (u *Uploader) upload(ctx context.Context, uid gregor1.UID, convID chat1.ConversationID,
	outboxID chat1.OutboxID, title, filename string, metadata []byte,
	chatUI func(sessionID int) libkb.ChatUI) (res chan types.AttachmentUploadStatus, err error) {
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
		chatUI(0).ChatAttachmentUploadProgress(ctx, parg)
	}

	// preprocess asset (get content type, create preview if possible) arg.SessionID,
	var ures types.AttachmentUploadStatus
	ures.Metadata = metadata
	pre, err := PreprocessAsset(ctx, u.DebugLabeler, filename)
	if err != nil {
		return res, err
	}
	if pre.Preview != nil {
		u.Debug(ctx, "upload: created preview in preprocess")
		// Store the preview in pending storage
		if err := storage.NewPendingPreviews(u.G()).Put(ctx, outboxID, pre.Preview); err != nil {
			return res, err
		}
	}

	// upload attachment and (optional) preview concurrently
	var g errgroup.Group
	u.Debug(ctx, "upload: uploading assets")
	bgctx := context.Background()
	g.Go(func() error {
		chatUI(0).ChatAttachmentUploadStart(bgctx, pre.BaseMetadata(), 0)
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
		ures.Object, err = u.store.UploadAsset(bgctx, &task)
		chatUI(0).ChatAttachmentUploadDone(bgctx)
		if err != nil {
			u.Debug(bgctx, "upload: error uploading primary asset to s3: %s", err)
		} else {
			ures.Object.Title = title
			ures.Object.MimeType = pre.ContentType
			ures.Object.Metadata = pre.BaseMetadata()
		}
		return err
	})

	if pre.Preview != nil {
		g.Go(func() error {
			chatUI(0).ChatAttachmentPreviewUploadStart(bgctx, pre.PreviewMetadata())
			// copy the params so as not to mess with the main params above
			previewParams := params

			// add preview suffix to object key (P in hex)
			// the s3path in gregor is expecting hex here
			previewParams.ObjectKey += "50"
			task := UploadTask{
				S3Params:       previewParams,
				Filename:       filename,
				FileSize:       len(pre.Preview),
				Plaintext:      newBufReadResetter(pre.Preview),
				S3Signer:       u.s3signer,
				ConversationID: convID,
				UserID:         uid,
				Progress:       progress,
			}
			preview, err := u.store.UploadAsset(bgctx, &task)
			chatUI(0).ChatAttachmentPreviewUploadDone(bgctx)
			if err == nil {
				ures.Preview = &preview
				ures.Preview.MimeType = pre.ContentType
				ures.Preview.Metadata = pre.PreviewMetadata()
				ures.Preview.Tag = chat1.AssetTag_PRIMARY
			} else {
				u.Debug(bgctx, "upload: error uploading preview asset to s3: %s", err)
			}
			return err
		})
	} else {
		g.Go(func() error {
			chatUI(0).ChatAttachmentPreviewUploadStart(bgctx, chat1.AssetMetadata{})
			chatUI(0).ChatAttachmentPreviewUploadDone(bgctx)
			return nil
		})
	}
	res = make(chan types.AttachmentUploadStatus, 1)
	go func() {
		if err := g.Wait(); err != nil {
			ures.Status = types.AttachmentUploaderTaskStatusFailed
			ures.Error = new(string)
			*ures.Error = err.Error()
		}
		ures.Status = types.AttachmentUploaderTaskStatusSuccess
		if err := u.setStatus(bgctx, outboxID, ures); err != nil {
			u.Debug(bgctx, "failed to set status on upload success: %s", err)
		}
		u.Debug(bgctx, "upload: upload complete: status: %v err: %s", ures.Status, ures.Error)
		// Ping Deliverer to notify that some of the message in the outbox might be read to send
		u.G().MessageDeliverer.ForceDeliverLoop(bgctx)
		res <- ures
	}()
	return res, nil
}
