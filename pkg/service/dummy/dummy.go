package dummy

import (
	"crypto/sha256"
	"fmt"

	"github.com/Glimesh/waveguide/pkg/control"
	"github.com/sirupsen/logrus"
)

type Service struct {
	config *Config
	log    logrus.FieldLogger
}

type Config struct {
	Address      string
	ClientID     string
	ClientSecret string
}

func New(config Config) *Service {
	return &Service{
		config: &config,
	}
}

func (s *Service) SetLogger(log logrus.FieldLogger) {
	s.log = log
}

func (s *Service) Name() string {
	return "Dummy Service"
}

func (s *Service) Connect() error {
	return nil
}

// GetHmacKey returns a sha256 string of the encoded channel ID
func (s *Service) GetHmacKey(channelID control.ChannelID) ([]byte, error) {
	h := sha256.New()
	h.Write([]byte(fmt.Sprint(channelID)))
	hmacKey := fmt.Sprintf("%x", h.Sum(nil))
	s.log.Debugf("Dummy service key for %d is %s", channelID, hmacKey)
	return []byte(hmacKey), nil
}

func (s *Service) StartStream(channelID control.ChannelID) (control.StreamID, error) {
	return control.StreamID(channelID + 1), nil
}

func (s *Service) EndStream(streamID control.StreamID) error {
	return nil
}

type StreamMetadataInput control.StreamMetadata

func (s *Service) UpdateStreamMetadata(streamID control.StreamID, metadata control.StreamMetadata) error {
	return nil
}

func (s *Service) SendJpegPreviewImage(streamID control.StreamID, img []byte) error {
	return nil
}
