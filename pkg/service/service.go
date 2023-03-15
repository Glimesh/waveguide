package service

import (
	"github.com/Glimesh/waveguide/config"
	"github.com/Glimesh/waveguide/pkg/service/dummy"
	"github.com/Glimesh/waveguide/pkg/service/glimesh"
	"github.com/Glimesh/waveguide/pkg/types"

	"github.com/sirupsen/logrus"
)

type Service interface {
	SetLogger(log logrus.FieldLogger)

	// Name of the service, eg: Glimesh
	Name() string
	// Connect to the service
	Connect() error
	// GetHmacKey Get the private HMAC key for a given channel ID
	GetHmacKey(channelID types.ChannelID) ([]byte, error)
	// StartStream Starts a stream for a given channel
	StartStream(channelID types.ChannelID) (types.StreamID, error)
	// EndStream Marks the given stream ID as ended on the service
	EndStream(streamID types.StreamID) error
	// UpdateStreamMetadata Updates the service with additional metadata about a stream
	UpdateStreamMetadata(streamID types.StreamID, metadata types.StreamMetadata) error
	// SendJpegPreviewImage Sends a JPEG preview image of a stream to the service
	SendJpegPreviewImage(streamID types.StreamID, img []byte) error
}

func New(cfg config.Config, logger *logrus.Logger) Service {
	svcCfg := cfg.Service
	var svc Service

	switch svcCfg.Type {
	case "dummy":
		svc = dummy.New(dummy.Config{})
	case "glimesh":
		svc = glimesh.New(svcCfg.Endpoint, svcCfg.ClientID, svcCfg.ClientSecret)
	default:
		panic("unsupported service type")
	}

	svc.SetLogger(logger.WithFields(logrus.Fields{
		"service": svc.Name(),
	}))

	return svc
}
