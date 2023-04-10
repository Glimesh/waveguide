package control

import (
	"time"

	"github.com/pion/rtp"
)

func (s *Stream) thumbnailer(done chan struct{}) {
OUTER:
	for {
		s.log.Debug("waiting for thumbnail request signal")
		select {
		case <-s.requestThumbnail:
		case <-done:
			break OUTER
		}
		s.log.Debug("thumbnail request received")

		for len(s.thumbnailReceiver) > 0 {
			<-s.thumbnailReceiver
		}
		s.log.Debug("thumbnail buffer drained")

		var pkt *rtp.Packet

		t := time.Now()
	INNER:
		for {
			select {
			case pkt = <-s.thumbnailReceiver:
			case <-done:
				s.log.Debug("stopping thumbnail receiver")
				break OUTER
			}

			select {
			case <-done:
				break OUTER
			default:
				// use a deadline of 10 seconds to retrieve a keyframe
				if time.Since(t) > time.Second*10 {
					s.log.Warn("keyframe not available")
					break INNER
				}
				keyframe := s.kf.GetKeyframe(pkt)
				if keyframe != nil {
					s.log.Debug("got keyframe")
					s.lastThumbnail <- keyframe
					s.log.Debug("sent keyframe")
					// reset and sleep after sending one keyframe
					s.kf.Reset()
					break INNER
				}
			}
		}
	}
	s.log.Debug("ending thumbnailer")
}
