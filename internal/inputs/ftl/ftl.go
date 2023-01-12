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

	videoTrack *webrtc.TrackLocalStaticRTP
	audioTrack *webrtc.TrackLocalStaticRTP

	videoChan chan *rtp.Packet
}

func (c *connHandler) OnConnect(channelID ftlproto.ChannelID) error {
	c.channelID = control.ChannelID(channelID)

	var err error
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

	c.control.AddTrack(c.channelID, c.videoTrack, webrtc.MimeTypeH264)
	c.control.AddTrack(c.channelID, c.audioTrack, webrtc.MimeTypeOpus)

	return nil
}

func (c *connHandler) GetHmacKey() (string, error) {
	return c.control.GetHmacKey(c.channelID)
}

func (c *connHandler) OnPlay(metadata ftlproto.FtlConnectionMetadata) error {
	c.control.ReportMetadata(c.channelID,
		control.ClientVendorNameMetadata(metadata.VendorName),
		control.ClientVendorVersionMetadata(metadata.VendorVersion),
	)

	c.control.StartStream(c.channelID)

	c.control.ReportMetadata(c.channelID, control.AudioCodecMetadata(webrtc.MimeTypeOpus))
	c.control.ReportMetadata(c.channelID, control.VideoCodecMetadata(webrtc.MimeTypeH264))

	return nil
}

func (c *connHandler) OnAudio(packet *rtp.Packet) error {
	c.control.ReportMetadata(c.channelID, control.AudioPacketsMetadata(len(packet.Payload)))
	return c.audioTrack.WriteRTP(packet)
}

func (c *connHandler) OnVideo(packet *rtp.Packet) error {
	// Use a lossy channel to send packets to snapshot handler
	// We don't want to block and queue up old data
	c.control.ReportVideoPacket(c.channelID, packet)

	c.control.ReportMetadata(c.channelID, control.VideoPacketsMetadata(len(packet.Payload)))
	return c.videoTrack.WriteRTP(packet)
}

func (c *connHandler) OnClose() {
	c.control.StopStream(c.channelID)
}
