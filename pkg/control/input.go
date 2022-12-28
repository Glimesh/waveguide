package control

import (
	"context"

	"github.com/sirupsen/logrus"
)

type Input interface {
	SetControl(ctrl *Control)
	SetLogger(log logrus.FieldLogger)

	Listen(ctx context.Context)

	// // Blocking Functions
	// // These functions are gatekeepers to the connection

	// // Authenticate is called after the user connects, and is ready to authenticate.
	// // If the user authenticates successfully, the function will return nil
	// // If the user does not authenticate successfully, the function will return errors.New("some error")
	// Authenticate(streamKey string) error

	// // ReadyForMedia is called when the media server is completely ready to send packets our way
	// // Needs to set the channel.video and channel.audio to new tracks
	// ReadyForMedia() error

	// // Callback Functions
	// // These functions will be called accordingly, but their output will not be used to affect the connection

	// // OnConnect is called when the user connects, but before they are authenticated
	// OnConnect()

	// // OnStreamStart does something
	// OnStreamStart(channelID int, streamID int)
}

type InputConfig[C any] struct {
	ReadConfig func(map[string]interface{}) C
}

type InputType struct {
	Name       string
	New        func(*Control, interface{}) (Input, error)
	ReadConfig InputConfig[any]
	Options    []InputOption
	Config     interface{}
}

type InputOption struct {
	Name     string
	Default  interface{}
	Value    interface{}
	Required bool
}
