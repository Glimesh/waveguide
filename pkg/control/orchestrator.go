package control

type Orchestrator interface {
	// Name of the service, eg: Glimesh
	Name() string
	// Connect to the service
	Connect() error
	// Close the service connection
	Close() error

	// TODO: Consider removing as public method
	// SendMessage(messageType uint8, payload []byte) error

	StartStream(channelID ChannelID, streamID StreamID) error
	StopStream(channelID ChannelID, streamID StreamID) error

	// TODO: Be less specific to the FTL Orchestrator
	// SendIntro(message interface{})
	// SendOutro(message interface{})
	// SendNodeState(message interface{})
	// SendChannelSubscription(message interface{})
	// SendStreamPublishing(message interface{})
	// SendStreamRelaying(message interface{})
}
