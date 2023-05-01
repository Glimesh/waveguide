package disk

import (
	"errors"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
	"github.com/pion/webrtc/v3/pkg/media/h264writer"
)

// TODO: figure out if wrapping this is needed
// Wrapped media.Writer into a custom type
// that may be useful later on
type writer struct {
	media.Writer
}

func (w *writer) WriteRTP(p *rtp.Packet) error {
	return w.Writer.WriteRTP(p)
}

func (w *writer) Close() error {
	return w.Writer.Close()
}

func NewVideoWriter(codec, filename string) (Writer, error) {
	switch codec {
	case webrtc.MimeTypeH264:
		w, err := h264writer.New(filename)
		if err != nil {
			return nil, err
		}
		return &writer{w}, nil
	case webrtc.MimeTypeVP8:
		// TODO: implement vp8
		panic("vp8 file output unimplemented")
	default:
		return nil, errors.New("unsupported codec")
	}
}
