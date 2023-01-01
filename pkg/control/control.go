package control

import (
	"errors"
	"time"

	"github.com/pion/webrtc/v3"
)

type Pipe struct {
	Input        string
	Output       string
	Orchestrator string
}

type Control struct {
	service      Service
	orchestrator Orchestrator
	streams      map[ChannelID]*Stream
}

func New() *Control {
	return &Control{
		// orchestrator: orchestrator,
		// service:         service,
		streams: make(map[ChannelID]*Stream),
	}
}

func (mgr *Control) SetService(service Service) {
	mgr.service = service
}

func (mgr *Control) SetOrchestrator(orch Orchestrator) {
	mgr.orchestrator = orch
}

func (mgr *Control) AddTrack(channelID ChannelID, track webrtc.TrackLocal) error {
	stream, err := mgr.getStream(channelID)
	if err != nil {
		return err
	}

	if track.Kind() == webrtc.RTPCodecTypeAudio {
		stream.hasSomeAudio = true
	} else if track.Kind() == webrtc.RTPCodecTypeVideo {
		stream.hasSomeVideo = true
	}

	stream.tracks = append(stream.tracks, track)

	return nil
}

func (mgr *Control) GetTracks(channelID ChannelID) ([]webrtc.TrackLocal, error) {
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

	streamID, err := mgr.service.StartStream(channelID)
	if err != nil {
		return &Stream{}, err
	}

	stream.StreamID = streamID

	err = mgr.orchestrator.StartStream(stream.ChannelID, stream.StreamID)
	if err != nil {
		return &Stream{}, err
	}

	ticker := time.NewTicker(30 * time.Second)
	go func() {
		for {
			select {
			case <-ticker.C:
				// Send orchestrator heartbeat
				mgr.orchestrator.Heartbeat(channelID)
			case <-stream.stopHeartbeat:
				ticker.Stop()
				return
			}
		}
	}()

	return stream, err
}

func (mgr *Control) StopStream(channelID ChannelID) (err error) {
	stream, err := mgr.getStream(channelID)
	if err != nil {
		return err
	}

	stream.stopHeartbeat <- true

	// Tell the orchestrator the stream has ended
	if err := mgr.orchestrator.StopStream(stream.ChannelID, stream.StreamID); err != nil {
		return err
	}

	// Tell the service the stream has ended
	if err := mgr.service.EndStream(stream.StreamID); err != nil {
		return err
	}

	return mgr.removeStream(channelID)
}

func (mgr *Control) SendThumbnail() error {
	return nil
}

func (mgr *Control) SendMetadata() error {
	return nil
}

func (mgr *Control) newStream(channelID ChannelID) (*Stream, error) {
	stream := &Stream{
		authenticated: true,
		mediaStarted:  false,
		ChannelID:     channelID,
		stopHeartbeat: make(chan bool, 1),
	}

	if _, exists := mgr.streams[channelID]; exists {
		return stream, errors.New("stream already exists in stream manager state")
	}
	mgr.streams[channelID] = stream

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

// func (mgr *Control) WatchChannel(channelID ChannelID, clientConnection *webrtc.PeerConnection) {
// 	// Here is where we ask the orchestrator where the channel / stream is
// 	// For now it's just local to the control though

// 	// Since we're local we take a shortcut
// 	serverConnection := mgr.peerConnections[channelID]

// 	// Offer
// 	offer, err := serverConnection.CreateOffer(nil)
// 	if err != nil {
// 		panic(err)
// 	}
// 	gatherComplete := webrtc.GatheringCompletePromise(serverConnection)
// 	if err := serverConnection.SetLocalDescription(offer); err != nil {
// 		panic(err)
// 	}
// 	<-gatherComplete
// 	offer = *serverConnection.LocalDescription()
// 	// fmt.Printf("Offer: %s\n", offer.SDP)

// 	// Answer
// 	fmt.Println("Before SetRemoteDescription")
// 	if err := clientConnection.SetRemoteDescription(offer); err != nil {
// 		panic(err)
// 	}
// 	fmt.Println("Before CreateAnswer")
// 	answer, err := clientConnection.CreateAnswer(nil)
// 	if err != nil {
// 		panic(err)
// 	}
// 	fmt.Printf("Answer: %s\n", answer.SDP)
// 	fmt.Println("Before SetLocalDescription")
// 	gather2Complete := webrtc.GatheringCompletePromise(clientConnection)
// 	if err := clientConnection.SetLocalDescription(answer); err != nil {
// 		panic(err)
// 	}
// 	<-gather2Complete
// 	clientDescription := *clientConnection.LocalDescription()
// 	fmt.Println("After SetLocalDescription")
// 	if err := serverConnection.SetRemoteDescription(clientDescription); err != nil {
// 		panic(err)
// 	}
// 	fmt.Println("After SetRemoteDescription")

// 	// serverConnection.SetRemoteDescription(*clientDescription)

// 	// Should be negotiated at this point
// 	fmt.Println("Negotiated WebRTC connection between Input and Output")

// 	// for _, candidate := range mgr.iceCandidates[channelID] {
// 	// 	fmt.Printf("Adding client ice %v\n", candidate)
// 	// 	// clientConnection.AddICECandidate(candidate)
// 	// }
// }

// func (mgr *Control) GetPeerConnection(channelID ChannelID) *webrtc.PeerConnection {
// 	return mgr.peerConnections[channelID]
// }
