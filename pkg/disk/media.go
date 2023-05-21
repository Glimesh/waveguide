package disk

import (
	"errors"
	"fmt"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
	"github.com/pion/webrtc/v3/pkg/media/h264writer"
	"github.com/pion/webrtc/v3/pkg/media/oggwriter"
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

// NewVideoWriter returns a new video writer for the supplied codec
// The writer write the raw encoded stream to disk with the stream ID
// prepended to the codec name e.g. for an h264 stream id "foo"
// the filename is "foo.h264"
func NewVideoWriter(codec, streamID string) (Writer, error) {
	filename := fmt.Sprintf("%s.h264", streamID)
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

const (
	audioSampleRate   = 48000
	audioChannelCount = 2
)

// NewAudioWriter returns a new audio writer for the supplied codec
// The writer write the raw encoded stream to disk with the stream ID
// prepended to the codec name e.g. for an ogg stream id "foo"
// the filename is "foo.ogg"
func NewAudioWriter(codec, streamID string) (Writer, error) {
	filename := fmt.Sprintf("%s.ogg", streamID)
	switch codec {
	case webrtc.MimeTypeOpus:
		w, err := oggwriter.New(filename, audioSampleRate, audioChannelCount)
		if err != nil {
			return nil, err
		}
		return &writer{w}, nil
	// TODO: support more audio codec?
	// case
	default:
		return nil, errors.New("unsupported codec")
	}
}
