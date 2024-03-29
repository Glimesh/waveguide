package hls

import (
	"context"

	"github.com/Glimesh/waveguide/pkg/control"

	"github.com/sirupsen/logrus"
)

type Server struct {
	log     logrus.FieldLogger
	control *control.Control

	// Listen address of the HLS webserver
	Address string
}

func New(address string) *Server {
	return &Server{
		Address: address,
	}
}

func (s *Server) SetControl(ctrl *control.Control) {
	s.control = ctrl
}

func (s *Server) SetLogger(log logrus.FieldLogger) {
	s.log = log
}

func (s *Server) Listen(ctx context.Context) {
	s.log.Infof("Starting HLS Server on %s", s.Address)

	// Basically what the HLS server needs to do is:
	// When a new stream is added:
	//  0. create a new file
	//  1. consume the video / audio from the stream
	//  2. write that video / audio directly to the HLS file

	// var b bytes.Buffer

	// s.control.AddMediaHandler()

	// b.read

	// s.control.Add
}
