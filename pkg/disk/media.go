package disk

import (
	"errors"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
	"github.com/pion/webrtc/v3/pkg/media/h264writer"
)

var (
	_ VideoWriter = (*noopWriter)(nil)
	_ AudioWriter = (*noopWriter)(nil)

	_ VideoWriter = (*videoWriter)(nil)
)

type noopWriter struct{}

func NewNoopVideoWriter() VideoWriter {
	return &noopWriter{}
}

func NewNoopAudioWriter() AudioWriter {
	return &noopWriter{}
}

func (nw *noopWriter) WriteVideo(_ *rtp.Packet) error { return nil }
func (nw *noopWriter) Close() error                   { return nil }

func (nw *noopWriter) WriteAudio() {}

// TODO: figure out if wrapping this is needed
// Wrapped media.Writer into a custom type
// that may be useful later on
type videoWriter struct {
	media.Writer
}

func (w *videoWriter) WriteVideo(p *rtp.Packet) error {
	return w.WriteRTP(p)
}

func (w *videoWriter) Close() error {
	return w.Writer.Close()
}

func NewVideoWriter(codec, filename string) (VideoWriter, error) {
	switch codec {
	case webrtc.MimeTypeH264:
		w, err := h264writer.New(filename)
		if err != nil {
			return nil, err
		}
		return &videoWriter{w}, nil
	case webrtc.MimeTypeVP8:
		// TODO: implement vp8
		panic("vp8 file output unimplemented")
	default:
		return nil, errors.New("unsupported codec")
	}
}
