package ftl

import (
	"context"
	"io"
	"log"
	"net"

	"github.com/Glimesh/waveguide/pkg/control"
	ftlproto "github.com/Glimesh/waveguide/pkg/protocols/ftl"
	"github.com/pion/webrtc/v3"
	"github.com/sirupsen/logrus"
)

type FTLSource struct {
	log     logrus.FieldLogger
	config  FTLSourceConfig
	control *control.Control
}

type FTLSourceConfig struct {
	Address string
}

func New(config FTLSourceConfig) *FTLSource {
	return &FTLSource{
		config: config,
	}
}

func (s *FTLSource) SetControl(ctrl *control.Control) {
	s.control = ctrl
}

func (s *FTLSource) SetLogger(log logrus.FieldLogger) {
	s.log = log
}

func (s *FTLSource) Listen(ctx context.Context) {
	log.Printf("Starting FTL server on %s", s.config.Address)

	tcpAddr, err := net.ResolveTCPAddr("tcp", s.config.Address)
	if err != nil {
		s.log.Errorf("Failed: %+v", err)
		return
	}

	listener, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		s.log.Errorf("Failed: %+v", err)
		return
	}

	s.log.Infof("Starting FTL Server on %s", s.config.Address)

	srv := ftlproto.NewServer(&ftlproto.ServerConfig{
		Log: s.log,
		OnConnect: func(conn net.Conn) (io.ReadWriteCloser, *ftlproto.ConnConfig) {
			return conn, &ftlproto.ConnConfig{
				Handler: &connHandler{
					control: s.control,
					log:     s.log,
				},
			}
		},
	})

	if err := srv.Serve(listener); err != nil {
		s.log.Panicf("Failed: %+v", err)
	}
}

type connHandler struct {
	control *control.Control
	log     logrus.FieldLogger

	channelID control.ChannelID
}

func (c *connHandler) OnConnect(channelID ftlproto.ChannelID) error {
	c.channelID = control.ChannelID(channelID)

	return nil
}

func (c *connHandler) GetHmacKey() (string, error) {
	return c.control.GetHmacKey(c.channelID)
}

func (c *connHandler) OnTracks(video *webrtc.TrackLocalStaticRTP, audio *webrtc.TrackLocalStaticRTP) error {

	c.control.StartStream(c.channelID)

	c.control.AddTrack(c.channelID, video, webrtc.MimeTypeH264)
	c.control.AddTrack(c.channelID, audio, webrtc.MimeTypeOpus)

	return nil
}

func (c *connHandler) OnPlay() error {

	return nil
}

func (c *connHandler) OnClose() {
	c.control.StopStream(c.channelID)
}
