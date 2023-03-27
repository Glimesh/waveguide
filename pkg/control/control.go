package control

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"net/http"
	"time"

	"github.com/Glimesh/waveguide/config"
	"github.com/Glimesh/waveguide/pkg/h264"
	"github.com/Glimesh/waveguide/pkg/orchestrator"
	"github.com/Glimesh/waveguide/pkg/service"
	"github.com/Glimesh/waveguide/pkg/types"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type Pipe struct {
	Input        string
	Output       string
	Orchestrator string
}

type Control struct {
	ctx                context.Context
	service            service.Service
	orchestrator       orchestrator.Orchestrator
	streams            map[types.ChannelID]*Stream
	metadataCollectors map[types.ChannelID]chan bool

	log     logrus.FieldLogger
	httpMux *http.ServeMux

	Hostname       string
	HTTPServerType string `mapstructure:"http_server_type"`
	HTTPAddress    string `mapstructure:"http_address"`
	HTTPS          bool
	HTTPSHostname  string `mapstructure:"https_hostname"`
	HTTPSCert      string `mapstructure:"https_cert"`
	HTTPSKey       string `mapstructure:"https_key"`
}

func New(
	ctx context.Context,
	cfg config.Config,
	hostname string,
	logger *logrus.Logger,
) (*Control, error) {
	svc := service.New(cfg, logger)
	if err := svc.Connect(); err != nil {
		return nil, fmt.Errorf("service: %w", err)
	}

	or := orchestrator.New(cfg, hostname, logger)
	if err := or.Connect(); err != nil {
		return nil, fmt.Errorf("orchestrator: %w", err)
	}

	httpCfg := cfg.Control

	return &Control{
		ctx:          ctx,
		service:      svc,
		orchestrator: or,

		streams:            make(map[types.ChannelID]*Stream),
		metadataCollectors: make(map[types.ChannelID]chan bool),
		httpMux:            http.NewServeMux(),
		log: logger.WithFields(logrus.Fields{
			"control": "waveguide",
		}),

		Hostname:       hostname,
		HTTPServerType: httpCfg.HTTPServerType,
		HTTPAddress:    httpCfg.Address,
		HTTPSHostname:  httpCfg.HTTPSHostname,
		HTTPSCert:      httpCfg.HTTPSCert,
		HTTPSKey:       httpCfg.HTTPSKey,
	}, nil
}

func (ctrl *Control) Context() context.Context {
	return ctrl.ctx
}

func (ctrl *Control) ContextErr() error {
	return ctrl.Context().Err()
}

func (ctrl *Control) Shutdown() {
	for c := range ctrl.streams {
		ctrl.StopStream(c)
	}
}

func (ctrl *Control) GetTracks(channelID types.ChannelID) ([]StreamTrack, error) {
	stream, err := ctrl.getStream(channelID)
	if err != nil {
		return nil, err
	}

	return stream.tracks, nil
}

func (ctrl *Control) GetHmacKey(channelID types.ChannelID) (string, error) {
	actualKey, err := ctrl.service.GetHmacKey(channelID)
	if err != nil {
		return "", err
	}

	return string(actualKey), nil
}

func (ctrl *Control) Authenticate(channelID types.ChannelID, streamKey types.StreamKey) error {
	actualKey, err := ctrl.service.GetHmacKey(channelID)
	if err != nil {
		return err
	}
	if string(streamKey) != string(actualKey) {
		return errors.New("incorrect stream key")
	}

	return nil
}

func (ctrl *Control) StartStream(channelID types.ChannelID) (*Stream, error) {
	ctx, cancel := context.WithCancel(ctrl.Context())

	stream, err := ctrl.newStream(channelID, cancel)
	if err != nil {
		return nil, err
	}

	ctrl.log.Infof("Starting stream for %s", channelID)

	streamID, err := ctrl.service.StartStream(channelID)
	if err != nil {
		ctrl.removeStream(channelID)
		return nil, err
	}
	stream.StreamID = streamID

	err = ctrl.orchestrator.StartStream(stream.ChannelID, stream.StreamID)
	if err != nil {
		ctrl.StopStream(channelID)
		return nil, err
	}

	go ctrl.setupHeartbeat(channelID)

	// Really gross, I'm sorry.
	whepEndpoint := fmt.Sprintf("%s/whep/endpoint", ctrl.HTTPServerURL())
	go func() {
		err := stream.thumbnailer(ctx, whepEndpoint)
		if err != nil {
			stream.log.Error(err)
			ctrl.StopStream(channelID)
		}
	}()

	return stream, err
}

func (ctrl *Control) StopStream(channelID types.ChannelID) error {
	// StopStream is called
	ctrl.log.Debug("Stop Stream")
	stream, err := ctrl.getStream(channelID)
	if err != nil {
		if errors.Is(err, errStreamRemoved) {
			return nil
		}
		return err
	}

	if !stream.Stopped() {
		stream.Stop()
	}
	ctrl.metadataCollectors[channelID] <- true

	// Make sure we send stop commands to everyone, and don't return until they've all been sent
	serviceErr := ctrl.service.EndStream(stream.StreamID)
	orchestratorErr := ctrl.orchestrator.StopStream(stream.ChannelID, stream.StreamID)
	controlErr := ctrl.removeStream(channelID)

	if serviceErr != nil {
		stream.log.Error(serviceErr)
		return serviceErr
	}
	if orchestratorErr != nil {
		stream.log.Error(orchestratorErr)
		return orchestratorErr
	}
	if controlErr != nil {
		stream.log.Error(controlErr)
		return controlErr
	}

	return nil
}

var (
	ErrHeartbeatThumbnail             = errors.New("error sending thumbnail")
	ErrHeartbeatSendMetadata          = errors.New("error sending metadata")
	ErrHeartbeatOrchestratorHeartbeat = errors.New("error sending orchestrator heartbeat")
)

func (ctrl *Control) setupHeartbeat(channelID types.ChannelID) {
	ticker := time.NewTicker(15 * time.Second)
	tickFailed := 0

	stream, err := ctrl.getStream(channelID)
	if err != nil {
		return
	}

	for {
		select {
		case <-ticker.C:
			stream.log.Infof("Collecting metadata tickFailed=%d", tickFailed)
			var err error
			hasErrors := false

			err = ctrl.sendThumbnail(channelID)
			if err != nil {
				stream.log.Error(errors.Wrap(err, ErrHeartbeatThumbnail.Error()))
				hasErrors = true
			}

			err = ctrl.sendMetadata(channelID)
			if err != nil {
				stream.log.Error(errors.Wrap(err, ErrHeartbeatSendMetadata.Error()))
				hasErrors = true
			}

			err = ctrl.orchestrator.Heartbeat(channelID)
			if err != nil {
				stream.log.Error(errors.Wrap(err, ErrHeartbeatOrchestratorHeartbeat.Error()))
				hasErrors = true
			}

			if hasErrors {
				tickFailed++
			} else if tickFailed > 0 {
				tickFailed--
			}

			// Look for 3 consecutive failures
			if tickFailed >= 5 {
				stream.log.Warn("Stopping stream due to excessive heartbeat errors")
				stream.Stop()
				ticker.Stop()
				return
			}

		case <-ctrl.metadataCollectors[channelID]:
			ticker.Stop()
			return
		}
	}
}

func (ctrl *Control) sendMetadata(channelID types.ChannelID) error {
	stream, err := ctrl.getStream(channelID)
	if err != nil {
		return err
	}

	stream.lastTime = time.Now().Unix()

	return ctrl.service.UpdateStreamMetadata(stream.StreamID, types.StreamMetadata{
		AudioCodec:        stream.audioCodec,
		IngestServer:      ctrl.Hostname,
		IngestViewers:     0,
		LostPackets:       0, // Don't exist
		NackPackets:       0, // Don't exist
		RecvPackets:       stream.totalAudioPackets + stream.totalVideoPackets,
		SourceBitrate:     0, // Likely just need to calculate the bytes between two 5s snapshots?
		SourcePing:        0, // Not accessible unless we ping them manually
		StreamTimeSeconds: int(stream.lastTime - stream.startTime),
		VendorName:        stream.clientVendorName,
		VendorVersion:     stream.clientVendorVersion,
		VideoCodec:        stream.videoCodec,
		VideoHeight:       stream.videoHeight,
		VideoWidth:        stream.videoWidth,
	})
}

func (ctrl *Control) sendThumbnail(channelID types.ChannelID) (err error) {
	stream, err := ctrl.getStream(channelID)
	if err != nil {
		return err
	}

	var data []byte
	// Since stream.lastThumbnail is a buffered chan, let's read all values to get the newest
	for len(stream.lastThumbnail) > 0 {
		data = <-stream.lastThumbnail
	}

	if len(data) == 0 {
		return nil
	}

	var img image.Image
	h264dec, err := h264.NewH264Decoder()
	if err != nil {
		return err
	}
	defer h264dec.Close()
	img, err = h264dec.Decode(data)
	if err != nil {
		return err
	}
	if img == nil {
		ctrl.log.WithField("channel_id", channelID).Debug("img is nil")
		return nil
	}

	buff := new(bytes.Buffer)
	err = jpeg.Encode(buff, img, &jpeg.Options{
		Quality: 75,
	})
	if err != nil {
		return err
	}

	err = ctrl.service.SendJpegPreviewImage(stream.StreamID, buff.Bytes())
	if err != nil {
		return err
	}

	ctrl.log.WithField("channel_id", channelID).Debug("Got screenshot!")

	// Also update our metadata
	stream.videoWidth = img.Bounds().Dx()
	stream.videoHeight = img.Bounds().Dy()

	return nil
}

func (ctrl *Control) newStream(channelID types.ChannelID, cancelFunc context.CancelFunc) (*Stream, error) {
	stream := &Stream{
		log: ctrl.log.WithField("channel_id", channelID),

		cancelFunc:      cancelFunc,
		authenticated:   true,
		mediaStarted:    false,
		ChannelID:       channelID,
		stopHeartbeat:   make(chan struct{}, 1),
		stopThumbnailer: make(chan struct{}, 1),
		// 10 keyframes in 5 seconds is probably a bit extreme
		lastThumbnail:       make(chan []byte, 10),
		startTime:           time.Now().Unix(),
		totalAudioPackets:   0,
		totalVideoPackets:   0,
		clientVendorName:    "",
		clientVendorVersion: "",
	}

	if _, exists := ctrl.streams[channelID]; exists {
		return stream, errors.New("stream already exists in stream manager state")
	}
	ctrl.streams[channelID] = stream
	ctrl.metadataCollectors[channelID] = make(chan bool, 1)

	return stream, nil
}

func (ctrl *Control) removeStream(id types.ChannelID) error {
	if _, exists := ctrl.streams[id]; !exists {
		return errors.New("RemoveStream stream does not exist in state")
	}

	delete(ctrl.streams, id)
	delete(ctrl.metadataCollectors, id)

	return nil
}

var errStreamRemoved = errors.New("stream does not exist in state")

func (ctrl *Control) getStream(id types.ChannelID) (*Stream, error) {
	if _, exists := ctrl.streams[id]; !exists {
		return nil, errStreamRemoved
	}
	return ctrl.streams[id], nil
}
