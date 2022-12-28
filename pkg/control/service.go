package control

import "github.com/sirupsen/logrus"

type Service interface {
	SetLogger(log logrus.FieldLogger)

	// Name of the service, eg: Glimesh
	Name() string
	// Connect to the service
	Connect() error
	// GetHmacKey Get the private HMAC key for a given channel ID
	GetHmacKey(channelID ChannelID) ([]byte, error)
	// StartStream Starts a stream for a given channel
	StartStream(channelID ChannelID) (StreamID, error)
	// EndStream Marks the given stream ID as ended on the service
	EndStream(streamID StreamID) error
	// UpdateStreamMetadata Updates the service with additional metadata about a stream
	UpdateStreamMetadata(streamID StreamID, metadata StreamMetadata) error
	// SendJpegPreviewImage Sends a JPEG preview image of a stream to the service
	SendJpegPreviewImage(streamID StreamID, img []byte) error
}
