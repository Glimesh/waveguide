package control

import "fmt"

type ChannelID uint32
type StreamID uint32
type StreamKey []byte

func (id ChannelID) String() string {
	return fmt.Sprintf("%d", id)
}

func (id StreamID) String() string {
	return fmt.Sprintf("%d", id)
}
