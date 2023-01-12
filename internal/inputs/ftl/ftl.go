package ftl

import (
	"context"
	"io"
	"net"

	"github.com/Glimesh/waveguide/pkg/control"
	ftlproto "github.com/Glimesh/waveguide/pkg/protocols/ftl"
	"github.com/pion/rtp"
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

	stream     *control.Stream
	videoTrack *webrtc.TrackLocalStaticRTP
	audioTrack *webrtc.TrackLocalStaticRTP
}

func (c *connHandler) OnConnect(channelID ftlproto.ChannelID) error {
	c.channelID = control.ChannelID(channelID)

	var err error
	c.stream, err = c.control.StartStream(c.channelID)
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
	err := c.audioTrack.WriteRTP(packet)

	c.stream.ReportMetadata(control.AudioPacketsMetadata(len(packet.Payload)))

	return err
}

func (c *connHandler) OnVideo(packet *rtp.Packet) error {
	// Write the RTP packet immediately, log after
	err := c.videoTrack.WriteRTP(packet)

	c.stream.ReportVideoPacket(packet)
	c.stream.ReportMetadata(control.VideoPacketsMetadata(len(packet.Payload)))

	return err
}

func (c *connHandler) OnClose() {
	c.control.StopStream(c.channelID)
}
