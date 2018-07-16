package attachments

import (
	"encoding/json"

	"github.com/keybase/client/go/protocol/chat1"
)

type Dimension struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

func (d *Dimension) Empty() bool {
	return d.Width == 0 && d.Height == 0
}

func (d *Dimension) Encode() string {
	if d.Width == 0 && d.Height == 0 {
		return ""
	}
	enc, err := json.Marshal(d)
	if err != nil {
		return ""
	}
	return string(enc)
}

type Preprocess struct {
	ContentType        string
	Preview            []byte
	PreviewContentType string
	BaseDim            *Dimension
	BaseDurationMs     int
	PreviewDim         *Dimension
	PreviewDurationMs  int
}

func (p *Preprocess) BaseMetadata() chat1.AssetMetadata {
	if p.BaseDim == nil || p.BaseDim.Empty() {
		return chat1.AssetMetadata{}
	}
	if p.BaseDurationMs > 0 {
		return chat1.NewAssetMetadataWithVideo(chat1.AssetMetadataVideo{
			Width:      p.BaseDim.Width,
			Height:     p.BaseDim.Height,
			DurationMs: p.BaseDurationMs,
		})
	}
	return chat1.NewAssetMetadataWithImage(chat1.AssetMetadataImage{
		Width:  p.BaseDim.Width,
		Height: p.BaseDim.Height,
	})
}

func (p *Preprocess) PreviewMetadata() chat1.AssetMetadata {
	if p.PreviewDim == nil || p.PreviewDim.Empty() {
		return chat1.AssetMetadata{}
	}
	if p.PreviewDurationMs > 0 {
		return chat1.NewAssetMetadataWithVideo(chat1.AssetMetadataVideo{
			Width:      p.PreviewDim.Width,
			Height:     p.PreviewDim.Height,
			DurationMs: p.PreviewDurationMs,
		})
	}
	return chat1.NewAssetMetadataWithImage(chat1.AssetMetadataImage{
		Width:  p.PreviewDim.Width,
		Height: p.PreviewDim.Height,
	})
}
