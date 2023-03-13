package ftl

import (
	"context"
	"net"

	"github.com/Glimesh/waveguide/pkg/control"
	ftlproto "github.com/Glimesh/waveguide/pkg/protocols/ftl"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3"
	"github.com/sirupsen/logrus"
)

type FTLSource struct {
	log     logrus.FieldLogger
	control *control.Control

	Address string
}

func New(address string) *FTLSource {
	return &FTLSource{
		Address: address,
	}
}

func (s *FTLSource) SetControl(ctrl *control.Control) {
	s.control = ctrl
}

func (s *FTLSource) SetLogger(log logrus.FieldLogger) {
	s.log = log
}

func (s *FTLSource) Listen(ctx context.Context) {
	tcpAddr, err := net.ResolveTCPAddr("tcp", s.Address)
	if err != nil {
		s.log.Errorf("Failed: %+v", err)
		return
	}

	listener, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		s.log.Errorf("Failed: %+v", err)
		return
	}

	s.log.Infof("Starting FTL Server on %s", s.Address)

	srv := ftlproto.NewServer(&ftlproto.ServerConfig{
		Log: s.log,
		OnNewConnect: func(conn net.Conn) (net.Conn, *ftlproto.ConnConfig) {
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
	control    *control.Control
	log        logrus.FieldLogger
	controlCtx context.Context

	channelID control.ChannelID

	stream     *control.Stream
	videoTrack *webrtc.TrackLocalStaticRTP
	audioTrack *webrtc.TrackLocalStaticRTP

	cancel chan bool
}

func (c *connHandler) OnConnect(channelID ftlproto.ChannelID) error {
	c.channelID = control.ChannelID(channelID)

	var err error
	c.stream, c.controlCtx, err = c.control.StartStream(c.channelID)
	if err != nil {
		return err
	}

	// Create a video track
	c.videoTrack, err = webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: "video/h264"}, "video", "pion")
	if err != nil {
		return err
	}

	// Create an audio track
	c.audioTrack, err = webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: "audio/opus"}, "audio", "pion")
	if err != nil {
		return err
	}

	c.stream.AddTrack(c.videoTrack, webrtc.MimeTypeH264)
	c.stream.AddTrack(c.audioTrack, webrtc.MimeTypeOpus)

	c.stream.ReportMetadata(
		control.AudioCodecMetadata(webrtc.MimeTypeOpus),
		control.VideoCodecMetadata(webrtc.MimeTypeH264),
	)

	return nil
}

func (c *connHandler) GetHmacKey() (string, error) {
	return c.control.GetHmacKey(c.channelID)
}

func (c *connHandler) OnPlay(metadata ftlproto.FtlConnectionMetadata) error {
	c.stream.ReportMetadata(
		control.ClientVendorNameMetadata(metadata.VendorName),
		control.ClientVendorVersionMetadata(metadata.VendorVersion),
	)

	return nil
}

func (c *connHandler) OnAudio(packet *rtp.Packet) error {
	if c.controlCtx.Err() != nil {
		return c.controlCtx.Err()
	}

	err := c.audioTrack.WriteRTP(packet)

	c.stream.ReportMetadata(control.AudioPacketsMetadata(len(packet.Payload)))

	return err
}

func (c *connHandler) OnVideo(packet *rtp.Packet) error {
	if c.controlCtx.Err() != nil {
		return c.controlCtx.Err()
	}

	// Write the RTP packet immediately, log after
	err := c.videoTrack.WriteRTP(packet)

	c.stream.ReportMetadata(control.VideoPacketsMetadata(len(packet.Payload)))

	return err
}

func (c *connHandler) OnClose() {
	if c.controlCtx.Err() == nil {
		// This is the FTL => Control cancellation
		// Only since if we're not the canceller.
		c.control.StopStream(c.channelID)
	}
}
