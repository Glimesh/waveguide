package control

import (
	"errors"
	"fmt"

	"github.com/pion/webrtc/v3"
)

type Pipe struct {
	Input        string
	Output       string
	Orchestrator string
}

type Control struct {
	service         Service
	orchestrator    Orchestrator
	streams         map[ChannelID]*Stream
	pipes           map[string]Pipe
	tracks          map[ChannelID][]webrtc.TrackLocal
	peerConnections map[ChannelID]*webrtc.PeerConnection
}

func New() *Control {
	return &Control{
		// orchestrator: orchestrator,
		// service:         service,
		streams:         make(map[ChannelID]*Stream),
		pipes:           make(map[string]Pipe),
		tracks:          make(map[ChannelID][]webrtc.TrackLocal),
		peerConnections: make(map[ChannelID]*webrtc.PeerConnection),
	}
}

func (mgr *Control) SetService(service Service) {
	mgr.service = service
}

func (mgr *Control) SetOrchestrator(orch Orchestrator) {
	mgr.orchestrator = orch
}

func (mgr *Control) AddPipe(pipe Pipe, input Input, output Output) {
	pipeName := pipe.Input + "-" + pipe.Output + "-"

	fmt.Println(pipeName)

	// mgr.pipes[pipeName] = pipe
	// Maybe a pipe creates a peer connection between two?

	mgr.pipes[pipeName] = pipe
}

func (mgr *Control) AddChannel(channelID ChannelID) {
	// peerConnection, err := api.NewPeerConnection(webrtc.Configuration{})
	// if err != nil {
	// 	panic(err)
	// }

	// // Set the handler for Peer connection state
	// // This will notify you when the peer has connected/disconnected
	// peerConnection.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
	// 	fmt.Printf("Peer Connection State has changed: %s (offerer)\n", s.String())

	// 	if s == webrtc.PeerConnectionStateFailed {
	// 		// Wait until PeerConnection has had no network activity for 30 seconds or another failure. It may be reconnected using an ICE Restart.
	// 		// Use webrtc.PeerConnectionStateDisconnected if you are interested in detecting faster timeout.
	// 		// Note that the PeerConnection may come back from PeerConnectionStateDisconnected.
	// 		fmt.Println("Peer Connection has gone to failed exiting")
	// 		// os.Exit(0)
	// 	}
	// })

	// mgr.peerConnections[channelID] = peerConnection
}

func (mgr *Control) WatchChannel(channelID ChannelID, clientConnection *webrtc.PeerConnection) {
	// Here is where we ask the orchestrator where the channel / stream is
	// For now it's just local to the control though

	// Since we're local we take a shortcut
	serverConnection := mgr.peerConnections[channelID]
	// dc, err := serverConnection.CreateDataChannel("initial_data_channel", nil)
	// if err != nil {
	// 	panic(err)
	// }
	// dc.OnMessage(func(msg webrtc.DataChannelMessage) {
	// 	fmt.Println("Got message from data channel")
	// })

	// Offer
	offer, err := serverConnection.CreateOffer(nil)
	if err != nil {
		panic(err)
	}
	gatherComplete := webrtc.GatheringCompletePromise(serverConnection)
	if err := serverConnection.SetLocalDescription(offer); err != nil {
		panic(err)
	}
	<-gatherComplete
	offer = *serverConnection.LocalDescription()
	// fmt.Printf("Offer: %s\n", offer.SDP)

	// Answer
	fmt.Println("Before SetRemoteDescription")
	if err := clientConnection.SetRemoteDescription(offer); err != nil {
		panic(err)
	}
	fmt.Println("Before CreateAnswer")
	answer, err := clientConnection.CreateAnswer(nil)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Answer: %s\n", answer.SDP)
	fmt.Println("Before SetLocalDescription")
	gather2Complete := webrtc.GatheringCompletePromise(clientConnection)
	if err := clientConnection.SetLocalDescription(answer); err != nil {
		panic(err)
	}
	<-gather2Complete
	clientDescription := *clientConnection.LocalDescription()
	fmt.Println("After SetLocalDescription")
	if err := serverConnection.SetRemoteDescription(clientDescription); err != nil {
		panic(err)
	}
	fmt.Println("After SetRemoteDescription")

	// serverConnection.SetRemoteDescription(*clientDescription)

	// Should be negotiated at this point
	fmt.Println("Negotiated WebRTC connection between Input and Output")

	// for _, candidate := range mgr.iceCandidates[channelID] {
	// 	fmt.Printf("Adding client ice %v\n", candidate)
	// 	// clientConnection.AddICECandidate(candidate)
	// }
}

func (mgr *Control) GetPeerConnection(channelID ChannelID) *webrtc.PeerConnection {
	return mgr.peerConnections[channelID]
}

func (mgr *Control) AddTrack(channelID ChannelID, track webrtc.TrackLocal) {
	mgr.tracks[channelID] = append(mgr.tracks[channelID], track)
	// mgr.peerConnections[channelID].AddTrack(track)
}

func (mgr *Control) GetTracks(channelID ChannelID) []webrtc.TrackLocal {
	// mgr.tracks[channelID] = append(mgr.tracks[channelID], track)
	return mgr.tracks[channelID]
}

func (mgr *Control) NewStream(channelID ChannelID) error {
	stream := &Stream{
		authenticated: false,
		mediaStarted:  false,
		ChannelID:     channelID,
	}

	if _, exists := mgr.streams[channelID]; exists {
		return errors.New("stream already exists in stream manager state")
	}
	mgr.streams[channelID] = stream

	return nil
}

func (mgr *Control) RemoveStream(id ChannelID) error {
	if _, exists := mgr.streams[id]; !exists {
		return errors.New("RemoveStream stream does not exist in state")
	}
	delete(mgr.streams, id)
	return nil
}
func (mgr *Control) GetStream(id ChannelID) (*Stream, error) {
	if _, exists := mgr.streams[id]; !exists {
		return &Stream{}, errors.New("GetStream stream does not exist in state")
	}
	return mgr.streams[id], nil
}

func (mgr *Control) GetHmacKey(channelID ChannelID) (string, error) {
	actualKey, err := mgr.service.GetHmacKey(channelID)
	if err != nil {
		return "", err
	}

	return string(actualKey), nil
}

func (mgr *Control) Authenticate(channelID ChannelID, streamKey StreamKey) error {
	stream, err := mgr.GetStream(channelID)
	if err != nil {
		return err
	}

	actualKey, err := mgr.service.GetHmacKey(channelID)
	if err != nil {
		return err
	}
	if string(streamKey) != string(actualKey) {
		return errors.New("incorrect stream key")
	}

	stream.authenticated = true
	stream.StreamKey = streamKey

	return nil
}

func (mgr *Control) StartStream(channelID ChannelID) (*Stream, error) {
	stream, err := mgr.GetStream(channelID)
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

	return stream, err
}

func (mgr *Control) StopStream(channelID ChannelID) (err error) {
	stream, err := mgr.GetStream(channelID)
	if err != nil {
		return err
	}

	// Tell the orchestrator the stream has ended
	if err := mgr.orchestrator.StopStream(stream.ChannelID, stream.StreamID); err != nil {
		return err
	}

	// Tell the service the stream has ended
	if err := mgr.service.EndStream(stream.StreamID); err != nil {
		return err
	}

	return nil
}

func (mgr *Control) SendThumbnail() error {
	return nil
}

func (mgr *Control) SendMetadata() error {
	return nil
}
