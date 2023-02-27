package control

import (
	"bytes"
	"image"
	"image/jpeg"
	"net/http"
	"time"

	"github.com/pkg/errors"

	"github.com/Glimesh/waveguide/pkg/h264"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v3/pkg/media/samplebuilder"
	"github.com/sirupsen/logrus"
)

type Pipe struct {
	Input        string
	Output       string
	Orchestrator string
}

type Control struct {
	log                logrus.FieldLogger
	service            Service
	orchestrator       Orchestrator
	streams            map[ChannelID]*Stream
	metadataCollectors map[ChannelID]chan bool

	config Config

	httpMux *http.ServeMux
}

type Config struct {
	Hostname       string
	HttpServerType string `mapstructure:"http_server_type"`
	HttpAddress    string `mapstructure:"http_address"`
	Https          bool
	HttpsHostname  string `mapstructure:"https_hostname"`
	HttpsCert      string `mapstructure:"https_cert"`
	HttpsKey       string `mapstructure:"https_key"`
}

func New(config Config) *Control {
	return &Control{
		config:             config,
		streams:            make(map[ChannelID]*Stream),
		metadataCollectors: make(map[ChannelID]chan bool),
		httpMux:            http.NewServeMux(),
	}
}

func (mgr *Control) Shutdown() {
	for c := range mgr.streams {
		mgr.StopStream(c)
	}
}

func (mgr *Control) SetLogger(logger logrus.FieldLogger) {
	mgr.log = logger
}
func (mgr *Control) SetService(service Service) {
	mgr.service = service
}

func (mgr *Control) SetOrchestrator(orch Orchestrator) {
	mgr.orchestrator = orch
}

func (mgr *Control) GetTracks(channelID ChannelID) ([]StreamTrack, error) {
	stream, err := mgr.getStream(channelID)
	if err != nil {
		return nil, err
	}

	return stream.tracks, nil
}

func (mgr *Control) GetHmacKey(channelID ChannelID) (string, error) {
	actualKey, err := mgr.service.GetHmacKey(channelID)
	if err != nil {
		return "", err
	}

	return string(actualKey), nil
}

func (mgr *Control) Authenticate(channelID ChannelID, streamKey StreamKey) error {
	actualKey, err := mgr.service.GetHmacKey(channelID)
	if err != nil {
		return err
	}
	if string(streamKey) != string(actualKey) {
		return errors.New("incorrect stream key")
	}

	return nil
}

func (mgr *Control) StartStream(channelID ChannelID) (*Stream, error) {
	stream, err := mgr.newStream(channelID)
	if err != nil {
		return &Stream{}, err
	}

	mgr.log.Infof("Starting stream for %s", channelID)

	streamID, err := mgr.service.StartStream(channelID)
	if err != nil {
		mgr.removeStream(channelID)
		return &Stream{}, err
	}

	stream.StreamID = streamID

	err = mgr.orchestrator.StartStream(stream.ChannelID, stream.StreamID)
	if err != nil {
		mgr.removeStream(channelID)
		return &Stream{}, err
	}

	go mgr.setupHeartbeat(channelID)
	go stream.KeyframeCollector()

	return stream, err
}

func (mgr *Control) StopStream(channelID ChannelID) (err error) {
	stream, err := mgr.getStream(channelID)
	if err != nil {
		return err
	}
	mgr.log.Infof("Stopping stream for %s", channelID)

	stream.stopHeartbeat <- true
	mgr.metadataCollectors[channelID] <- true

	// Make sure we send stop commands to everyone, and don't return until they've all been sent
	serviceErr := mgr.service.EndStream(stream.StreamID)
	orchestratorErr := mgr.orchestrator.StopStream(stream.ChannelID, stream.StreamID)
	controlErr := mgr.removeStream(channelID)

	if serviceErr != nil {
		return serviceErr
	}
	if orchestratorErr != nil {
		return orchestratorErr
	}

	return controlErr
}

var ErrHeartbeatThumbnail = errors.New("error sending thumbnail")
var ErrHeartbeatSendMetadata = errors.New("error sending metadata")
var ErrHeartbeatOrchestratorHeartbeat = errors.New("error sending orchestrator heartbeat")

func (mgr *Control) setupHeartbeat(channelID ChannelID) {
	ticker := time.NewTicker(15 * time.Second)
	go func() {
		tickFailed := 0
		streamLogger := mgr.log.WithField("channel_id", channelID)

		for {
			select {
			case <-ticker.C:
				streamLogger.Infof("Collecting metadata tickFailed=%d", tickFailed)
				var err error
				hasErrors := false

				err = mgr.sendThumbnail(channelID)
				if err != nil {
					streamLogger.Error(errors.Wrap(err, ErrHeartbeatThumbnail.Error()))
					hasErrors = true
				}

				err = mgr.sendMetadata(channelID)
				if err != nil {
					streamLogger.Error(errors.Wrap(err, ErrHeartbeatSendMetadata.Error()))
					hasErrors = true
				}

				err = mgr.orchestrator.Heartbeat(channelID)
				if err != nil {
					streamLogger.Error(errors.Wrap(err, ErrHeartbeatOrchestratorHeartbeat.Error()))
					hasErrors = true
				}

				if hasErrors {
					tickFailed += 1
				} else {
					if tickFailed > 0 {
						tickFailed -= 1
					}
				}

				// Look for 3 consecutive failures
				if tickFailed >= 5 {
					streamLogger.Warn("Stopping stream due to excessive heartbeat errors")
					mgr.StopStream(channelID)
					ticker.Stop()
					return
				}

			case <-mgr.metadataCollectors[channelID]:
				ticker.Stop()
				return
			}
		}
	}()
}

func (mgr *Control) sendMetadata(channelID ChannelID) error {
	stream, err := mgr.getStream(channelID)
	if err != nil {
		return err
	}

	stream.lastTime = time.Now().Unix()

	return mgr.service.UpdateStreamMetadata(stream.StreamID, StreamMetadata{
		AudioCodec:        stream.audioCodec,
		IngestServer:      mgr.config.Hostname,
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

func (mgr *Control) sendThumbnail(channelID ChannelID) (err error) {
	stream, err := mgr.getStream(channelID)
	if err != nil {
		return err
	}

	var data []byte
	if len(stream.lastKeyframe) > 0 {
		data = stream.lastKeyframe
	}
	// else {
	// 	sample := stream.videoSampler.Pop()
	// 	if sample == nil {
	// 		mgr.log.WithField("channel_id", channelID).Debug("Video sample is not ready yet")
	// 		return
	// 	}
	// 	data = sample.Data
	// }
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
		mgr.log.WithField("channel_id", channelID).Debug("img is nil")
		return nil
	}

	buff := new(bytes.Buffer)
	err = jpeg.Encode(buff, img, &jpeg.Options{
		Quality: 75,
	})
	if err != nil {
		return err
	}

	err = mgr.service.SendJpegPreviewImage(stream.StreamID, buff.Bytes())
	if err != nil {
		return err
	}

	mgr.log.WithField("channel_id", channelID).Debug("Got screenshot!")

	// Also update our metadata
	stream.videoWidth = img.Bounds().Dx()
	stream.videoHeight = img.Bounds().Dy()

	return nil
}

func (mgr *Control) newStream(channelID ChannelID) (*Stream, error) {
	stream := &Stream{
		authenticated:       true,
		mediaStarted:        false,
		ChannelID:           channelID,
		stopHeartbeat:       make(chan bool, 1),
		startTime:           time.Now().Unix(),
		totalAudioPackets:   0,
		totalVideoPackets:   0,
		clientVendorName:    "",
		clientVendorVersion: "",
		// recentVideoPackets:  make([]*rtp.Packet, 0),
		VideoPackets: make(chan *rtp.Packet, 512),
		videoSampler: samplebuilder.New(150, &codecs.H264Packet{}, 90000),
	}

	if _, exists := mgr.streams[channelID]; exists {
		return stream, errors.New("stream already exists in stream manager state")
	}
	mgr.streams[channelID] = stream
	mgr.metadataCollectors[channelID] = make(chan bool, 1)

	return stream, nil
}

func (mgr *Control) removeStream(id ChannelID) error {
	if _, exists := mgr.streams[id]; !exists {
		return errors.New("RemoveStream stream does not exist in state")
	}
	delete(mgr.streams, id)
	return nil
}

func (mgr *Control) getStream(id ChannelID) (*Stream, error) {
	if _, exists := mgr.streams[id]; !exists {
		return &Stream{}, errors.New("GetStream stream does not exist in state")
	}
	return mgr.streams[id], nil
}
