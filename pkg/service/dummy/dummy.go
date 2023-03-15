package dummy

import (
	"crypto/sha256"
	"fmt"

	"github.com/Glimesh/waveguide/pkg/types"
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
func (s *Service) GetHmacKey(channelID types.ChannelID) ([]byte, error) {
	h := sha256.New()
	h.Write([]byte(fmt.Sprint(channelID)))
	hmacKey := fmt.Sprintf("%x", h.Sum(nil))
	s.log.Debugf("Dummy service key for %d is %s", channelID, hmacKey)
	return []byte(hmacKey), nil
}

func (s *Service) StartStream(channelID types.ChannelID) (types.StreamID, error) {
	return types.StreamID(channelID + 1), nil
}

func (s *Service) EndStream(streamID types.StreamID) error {
	return nil
}

type StreamMetadataInput types.StreamMetadata

func (s *Service) UpdateStreamMetadata(streamID types.StreamID, metadata types.StreamMetadata) error {
	return nil
}

func (s *Service) SendJpegPreviewImage(streamID types.StreamID, img []byte) error {
	return nil
}
