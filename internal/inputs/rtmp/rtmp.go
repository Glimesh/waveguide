package rtmp

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

	"github.com/Glimesh/go-fdkaac/fdkaac"
	"github.com/Glimesh/waveguide/pkg/control"
	"github.com/Glimesh/waveguide/pkg/types"

	h264joy "github.com/nareix/joy5/codec/h264"
	rtp "github.com/pion/rtp"
	codecs "github.com/pion/rtp/codecs"
	webrtc "github.com/pion/webrtc/v3"
	logrus "github.com/sirupsen/logrus"
	flvtag "github.com/yutopp/go-flv/tag"
	gortmp "github.com/yutopp/go-rtmp"
	rtmpmsg "github.com/yutopp/go-rtmp/message"
	opus "gopkg.in/hraban/opus.v2"
)

const (
	FTL_MTU      uint16 = 1392
	FTL_VIDEO_PT uint8  = 96
	FTL_AUDIO_PT uint8  = 97

	BANDWIDTH_LIMIT int = 8000 * 1000
)

type Source struct {
	log     logrus.FieldLogger
	control *control.Control

	// Listen address of the RTMP server in the ip:port format
	Address string
}

func New(address string) *Source {
	return &Source{ //nolint exhaustive struct
		Address: address,
	}
}

func (s *Source) SetControl(ctrl *control.Control) {
	s.control = ctrl
}

func (s *Source) SetLogger(log logrus.FieldLogger) {
	s.log = log
}

func (s *Source) Listen(ctx context.Context) {
	tcpAddr, err := net.ResolveTCPAddr("tcp", s.Address)
	if err != nil {
		s.log.Errorf("Failed: %+v", err)
	}

	listener, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		s.log.Errorf("Failed: %+v", err)
	}

	s.log.Infof("Starting RTMP Server on %s", s.Address)

	srv := gortmp.NewServer(&gortmp.ServerConfig{
		OnConnect: func(conn net.Conn) (io.ReadWriteCloser, *gortmp.ConnConfig) {
			return conn, &gortmp.ConnConfig{ //nolint exhaustive struct
				Handler: &connHandler{ //nolint exhaustive struct
					control:                s.control,
					log:                    s.log,
					stopMetadataCollection: make(chan bool, 1),
				},

				ControlState: gortmp.StreamControlStateConfig{ //nolint exhaustive struct
					DefaultBandwidthWindowSize: 6 * 1024 * 1024 / 8,
				},
				Logger: s.log.WithField("app", "yutopp/go-rtmp"),
			}
		},
	})
	if err := srv.Serve(listener); err != nil {
		s.log.Panicf("Failed: %+v", err)
	}
}

type connHandler struct {
	gortmp.DefaultHandler
	control *control.Control
	// controlCtx context.Context

	log logrus.FieldLogger

	channelID        types.ChannelID
	streamID         types.StreamID
	streamKey        []byte
	started          bool
	authenticated    bool
	errored          bool
	metadataFailures int

	stream *control.Stream

	videoTrack *webrtc.TrackLocalStaticRTP
	audioTrack *webrtc.TrackLocalStaticRTP

	videoSequencer  rtp.Sequencer
	videoPacketizer rtp.Packetizer
	videoClockRate  uint32

	audioSequencer  rtp.Sequencer
	audioPacketizer rtp.Packetizer
	audioClockRate  uint32
	audioDecoder    *fdkaac.AacDecoder
	audioBuffer     []byte
	audioEncoder    *opus.Encoder

	keyframes       int
	lastKeyFrames   int
	lastInterFrames int

	stopMetadataCollection chan bool

	videoJoyCodec *h264joy.Codec
}

func (h *connHandler) OnServe(conn *gortmp.Conn) {
	h.log.Info("OnServe: %#v", conn)
}

func (h *connHandler) OnConnect(timestamp uint32, cmd *rtmpmsg.NetConnectionConnect) (err error) {
	h.log.Info("OnConnect: %#v", cmd)

	h.metadataFailures = 0
	h.errored = false

	h.videoClockRate = 90000
	// TODO: This can be customized by the user, we should figure out how to infer it from the client
	h.audioClockRate = 48000

	return nil
}

func (h *connHandler) OnCreateStream(timestamp uint32, cmd *rtmpmsg.NetConnectionCreateStream) error {
	h.log.Info("OnCreateStream: %#v", cmd)
	return nil
}

func (h *connHandler) OnPublish(ctx *gortmp.StreamContext, timestamp uint32, cmd *rtmpmsg.NetStreamPublish) (err error) {
	h.log.Info("OnPublish: %#v", cmd)

	if cmd.PublishingName == "" {
		return errors.New("PublishingName is empty")
	}
	// Authenticate
	auth := strings.SplitN(cmd.PublishingName, "-", 2)
	u64, err := strconv.ParseUint(auth[0], 10, 32)

	if err != nil {
		h.log.Error(err)
		return err
	}
	h.channelID = types.ChannelID(u64)
	h.streamKey = []byte(auth[1])

	h.started = true

	if err := h.control.Authenticate(h.channelID, h.streamKey); err != nil {
		h.log.Error(err)
		return err
	}

	h.stream, err = h.control.StartStream(h.channelID)
	if err != nil {
		h.log.Error(err)
		return err
	}

	h.authenticated = true

	h.streamID = h.stream.StreamID

	// Add some meta info to the logger
	h.log = h.log.WithFields(logrus.Fields{
		"channel_id": h.channelID,
		"stream_id":  h.streamID,
	})

	h.stream.ReportMetadata(
		control.ClientVendorNameMetadata("waveguide-rtmp-input"),
		control.ClientVendorVersionMetadata("0.0.1"),
	)

	if err := h.initVideo(h.videoClockRate); err != nil {
		return err
	}
	if err := h.initAudio(h.audioClockRate); err != nil {
		return err
	}

	return nil
}

func (h *connHandler) OnClose() {
	h.log.Info("RTMP OnClose")

	h.stopMetadataCollection <- true
	h.log.Debug("sent stop metadata collection signal")

	// We only want to publish the stop if it's ours
	// We also don't want control to stop the stream if we're respond to a stop
	if h.authenticated && h.control.ContextErr() == nil {
		// StopStream mainly calls external services, there's a chance this call can hang for a bit while the other services are processing
		// However it's not safe to call RemoveStream until this is finished or the pointer wont... exist?
		if err := h.control.StopStream(h.channelID); err != nil {
			h.log.Error(err)
			// panic(err)
		}
	}
	h.authenticated = false

	h.started = false

	if h.audioDecoder != nil {
		h.audioDecoder.Close()
		h.audioDecoder = nil
	}
}

func (h *connHandler) initAudio(clockRate uint32) (err error) {
	h.audioSequencer = rtp.NewFixedSequencer(0) // ftl client says this should be changed to a random value
	h.audioPacketizer = rtp.NewPacketizer(
		FTL_MTU,
		FTL_AUDIO_PT,
		uint32(h.channelID),
		&codecs.OpusPayloader{},
		h.audioSequencer,
		clockRate,
	)

	h.audioTrack, err = webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus}, //nolint exhaustive struct
		"audio",
		"pion",
	)
	if err != nil {
		return err
	}

	h.audioEncoder, err = opus.NewEncoder(int(clockRate), 2, opus.AppAudio)
	if err != nil {
		return err
	}
	h.audioDecoder = fdkaac.NewAacDecoder()

	h.stream.AddTrack(h.audioTrack, webrtc.MimeTypeOpus)
	h.stream.ReportMetadata(control.AudioCodecMetadata(webrtc.MimeTypeOpus))

	return nil
}

func (h *connHandler) OnAudio(timestamp uint32, payload io.Reader) error {
	if h.errored {
		return errors.New("stream is not longer authenticated")
	}
	if err := h.control.ContextErr(); err != nil {
		return err
	}
	if h.stream.Stopped() {
		return errors.New("stream terminated")
	}

	// Convert AAC to opus
	var audio flvtag.AudioData
	if err := flvtag.DecodeAudioData(payload, &audio); err != nil {
		return err
	}

	data, err := io.ReadAll(audio.Data)
	if err != nil {
		return err
	}

	if audio.AACPacketType == flvtag.AACPacketTypeSequenceHeader {
		h.log.Infof("Created new codec %s", hex.EncodeToString(data))
		err := h.audioDecoder.InitRaw(data)

		if err != nil {
			h.log.WithError(err).Errorf("error initializing stream")
			return fmt.Errorf("can't initialize codec with %s", hex.EncodeToString(data))
		}

		return nil
	}

	pcm, err := h.audioDecoder.Decode(data)
	if err != nil {
		h.log.Errorf("decode error: %s %s", hex.EncodeToString(data), err)
		return fmt.Errorf("decode error")
	}

	blockSize := 960
	for h.audioBuffer = append(h.audioBuffer, pcm...); len(h.audioBuffer) >= blockSize*4; h.audioBuffer = h.audioBuffer[blockSize*4:] {
		pcm16 := make([]int16, blockSize*2)
		for i := 0; i < len(pcm16); i++ {
			pcm16[i] = int16(binary.LittleEndian.Uint16(h.audioBuffer[i*2:]))
		}
		bufferSize := 1024
		opusData := make([]byte, bufferSize)
		n, err := h.audioEncoder.Encode(pcm16, opusData)
		if err != nil {
			return err
		}
		opusOutput := opusData[:n]

		packets := h.audioPacketizer.Packetize(opusOutput, uint32(blockSize))

		for _, p := range packets {
			if err := h.audioTrack.WriteRTP(p); err != nil {
				return err
			}
		}

		h.stream.ReportMetadata(control.AudioPacketsMetadata(len(packets))) //nolint exhaustive struct
	}

	return nil
}

func (h *connHandler) initVideo(clockRate uint32) error {
	h.videoSequencer = rtp.NewFixedSequencer(25000)
	h.videoPacketizer = rtp.NewPacketizer(
		FTL_MTU,
		FTL_VIDEO_PT,
		uint32(h.channelID+1),
		&codecs.H264Payloader{},
		h.videoSequencer, clockRate,
	)

	track, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264}, //nolint exhaustive struct
		"video",
		"pion",
	)
	if err != nil {
		return err
	}
	h.videoTrack = track
	h.stream.AddTrack(track, webrtc.MimeTypeH264)
	h.stream.ReportMetadata(control.VideoCodecMetadata(webrtc.MimeTypeH264))

	return nil
}

func (h *connHandler) OnVideo(timestamp uint32, payload io.Reader) error {
	if h.errored {
		return errors.New("stream is not longer authenticated")
	}
	if err := h.control.ContextErr(); err != nil {
		return err
	}
	if h.stream.Stopped() {
		return errors.New("stream terminated")
	}

	var video flvtag.VideoData
	if err := flvtag.DecodeVideoData(payload, &video); err != nil {
		return err
	}

	// video.CodecID == H264, I wonder if we should check this?
	// video.FrameType does not seem to contain b-frames even if they exist

	switch video.FrameType {
	case flvtag.FrameTypeKeyFrame:
		h.lastKeyFrames++
		h.keyframes++
	case flvtag.FrameTypeInterFrame:
		h.lastInterFrames++
	default:
		h.log.Debug("Unknown FLV Video Frame: %+v\n", video)
	}

	data, err := io.ReadAll(video.Data)
	if err != nil {
		return err
	}

	// From: https://github.com/nareix/joy5/blob/2c912ca30590ee653145d93873b0952716d21093/cmd/avtool/seqhdr.go#L38-L65
	// joy5 is an unlicensed project -- need to confirm usage.
	// Look at video.AVCPacketType == flvtag.AVCPacketTypeSequenceHeader to figure out sps and pps
	// Store those in the stream object, then use them later for the keyframes
	if video.AVCPacketType == flvtag.AVCPacketTypeSequenceHeader {
		h.videoJoyCodec, err = h264joy.FromDecoderConfig(data)
		if err != nil {
			return err
		}
	}

	var outBuf []byte
	if video.FrameType == flvtag.FrameTypeKeyFrame {
		// This fails ffprobe
		pktnalus, _ := h264joy.SplitNALUs(data)
		nalus := [][]byte{}
		nalus = append(nalus, h264joy.Map2arr(h.videoJoyCodec.SPS)...)
		nalus = append(nalus, h264joy.Map2arr(h.videoJoyCodec.PPS)...)
		nalus = append(nalus, pktnalus...)
		data := h264joy.JoinNALUsAnnexb(nalus)
		outBuf = data
	} else {
		pktnalus, _ := h264joy.SplitNALUs(data)
		data := h264joy.JoinNALUsAnnexb(pktnalus)
		outBuf = data
	}

	// Likely there's more than one set of RTP packets in this read
	samples := uint32(len(outBuf)) + h.videoClockRate
	packets := h.videoPacketizer.Packetize(outBuf, samples)

	for _, p := range packets {
		if err := h.videoTrack.WriteRTP(p); err != nil {
			return err
		}
	}

	h.stream.ReportMetadata(control.VideoPacketsMetadata(len(packets)))

	return nil
}
