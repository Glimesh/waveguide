package control

import (
	"github.com/Glimesh/waveguide/pkg/disk"

	"github.com/pion/rtp"
	"github.com/sirupsen/logrus"
)

type FileWriter interface {
	SendRTP(p *rtp.Packet)
	Run()
	Done()
}

type noopFileWriter struct{}

func (noop *noopFileWriter) SendRTP(_ *rtp.Packet) {}
func (noop *noopFileWriter) Run()                  {}
func (noop *noopFileWriter) Done()                 {}

type fileWriter struct {
	log      logrus.FieldLogger
	writer   disk.Writer
	packetCh chan *rtp.Packet
	done     chan struct{}
}

func (fw *fileWriter) SendRTP(p *rtp.Packet) {
	select {
	case fw.packetCh <- p:
	default:
	}
}

func (fw *fileWriter) Run() {
	fw.log.Debug("starting file writer")
LOOP:
	for {
		select {
		case <-fw.done:
			break LOOP
		case p := <-fw.packetCh:
			if err := fw.writer.WriteRTP(p); err != nil {
				fw.log.Debugf("writer: %v", err)
				break LOOP
			}
		}
	}
	fw.log.Debug("ending writer")
	fw.writer.Close()
}

func (fw *fileWriter) Done() {
	fw.done <- struct{}{}
}
