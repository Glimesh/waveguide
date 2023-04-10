package control

import "github.com/Glimesh/waveguide/pkg/disk"

func (s *Stream) configureVideoWriter(codec string) {
	videoWriter := disk.NewNoopVideoWriter()
	if s.saveVideo {
		if vw, err := disk.NewVideoWriter(codec, "out.h264"); err == nil {
			videoWriter = vw
		} else {
			s.log.Debug("video save enabled but failed to create video writer")
			s.log.Warnf("video writer: %v", err)
			s.log.Debug("falling back to noop video writer")
		}
	}
	s.videoWriter = videoWriter
}

func (s *Stream) writer(done chan struct{}) {
	s.log.Debug("starting file writer")
LOOP:
	for {
		select {
		case <-done:
			break LOOP
		case p := <-s.videoWriterChan:
			if err := s.videoWriter.WriteVideo(p); err != nil {
				s.log.Debugf("writer: %v", err)
				break LOOP
			}
		}
	}
	s.log.Debug("ending writer")
	s.videoWriter.Close()
}
