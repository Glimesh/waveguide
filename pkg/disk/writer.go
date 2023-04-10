package disk

import "github.com/pion/rtp"

type VideoWriter interface {
	WriteVideo(p *rtp.Packet) error
	Close() error
}

type AudioWriter interface {
	WriteAudio()
}
