package disk

import "github.com/pion/rtp"

type Writer interface {
	WriteRTP(p *rtp.Packet) error
	Close() error
}
