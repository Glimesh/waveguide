package control

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"time"

	"github.com/Glimesh/waveguide/pkg/h264"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media/samplebuilder"
)

type Pipe struct {
	Input        string
	Output       string
	Orchestrator string
}

type Control struct {
	hostname           string
	service            Service
	orchestrator       Orchestrator
	streams            map[ChannelID]*Stream
	metadataCollectors map[ChannelID]chan bool
}

func New(hostname string) *Control {
	return &Control{
		// orchestrator: orchestrator,
		// service:         service,
		streams:            make(map[ChannelID]*Stream),
		metadataCollectors: make(map[ChannelID]chan bool),
	}
}

func (mgr *Control) SetService(service Service) {
	mgr.service = service
}

func (mgr *Control) SetOrchestrator(orch Orchestrator) {
	mgr.orchestrator = orch
}

func (mgr *Control) AddTrack(channelID ChannelID, track webrtc.TrackLocal, codec string) error {
	stream, err := mgr.getStream(channelID)
	if err != nil {
		return err
	}

	// TODO: Needs better support for tracks with different codecs
	if track.Kind() == webrtc.RTPCodecTypeAudio {
		stream.hasSomeAudio = true
		stream.metadata.AudioCodec = codec
	} else if track.Kind() == webrtc.RTPCodecTypeVideo {
		stream.hasSomeVideo = true
		stream.metadata.VideoCodec = codec
	}

	stream.tracks = append(stream.tracks, StreamTrack{
		Type:  track.Kind(),
		Track: track,
		Codec: codec,
	})

	return nil
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

	streamID, err := mgr.service.StartStream(channelID)
	if err != nil {
		return &Stream{}, err
	}

	stream.StreamID = streamID

	err = mgr.orchestrator.StartStream(stream.ChannelID, stream.StreamID)
	if err != nil {
		return &Stream{}, err
	}

	go mgr.setupHeartbeat(channelID)

	return stream, err
}

func (mgr *Control) StopStream(channelID ChannelID) (err error) {
	stream, err := mgr.getStream(channelID)
	if err != nil {
		return err
	}

	stream.stopHeartbeat <- true
	mgr.metadataCollectors[channelID] <- true

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

func (mgr *Control) ReportMetadata(channelID ChannelID, metadatas ...Metadata) error {
	stream, err := mgr.getStream(channelID)
	if err != nil {
		return err
	}

	for _, metadata := range metadatas {
		metadata(stream)
	}

	return nil
}

// ReportVideoPacket is used to send the control packets that it should later use for
// image generation or other video needs.
// Should be refactored to be faster since it's called on every packet.
func (mgr *Control) ReportVideoPacket(channelID ChannelID, packet *rtp.Packet) error {
	stream, err := mgr.getStream(channelID)
	if err != nil {
		return err
	}

	if h264.IsKeyframePart(packet.Payload) {
		stream.recentVideoPackets = append(stream.recentVideoPackets, packet)
	}

	return nil
}

// ReportLastKeyframe works similar to ReportVideoPacket, except it's used in situations
// where we are converting from other video formats and we easily know the keyframes.
func (mgr *Control) ReportLastKeyframe(channelID ChannelID, keyframe []byte) error {
	stream, err := mgr.getStream(channelID)
	if err != nil {
		return err
	}

	stream.lastKeyframe = keyframe

	return nil
}

func (mgr *Control) setupHeartbeat(channelID ChannelID) {
	ticker := time.NewTicker(15 * time.Second)
	go func() {
		errors := 0
		// Todo: Move this somewhere else

		for {
			select {
			case <-ticker.C:
				var err error
				fmt.Println("start beat")

				fmt.Println("Sending thumbnail")
				err = mgr.sendThumbnail(channelID)
				if err != nil {
					fmt.Println(err)
				}

				fmt.Println("Sending metadata")
				err = mgr.sendMetadata(channelID)
				if err != nil {
					fmt.Println(err)
				}

				fmt.Println("Sending heartbeat")
				err = mgr.orchestrator.Heartbeat(channelID)
				if err != nil {
					fmt.Println(err)
				}

				if err != nil {
					// Close the stream
					fmt.Println("Stopping stream due to errors exceeding 5")
					errors += 1

				}
				if errors > 5 {
					mgr.StopStream(channelID)
					ticker.Stop()
					return
				}

				errors = 0
				fmt.Println("end beat")

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
		IngestServer:      mgr.hostname,
		IngestViewers:     0,
		LostPackets:       0, // Don't exist
		NackPackets:       0, // Don't exist
		RecvPackets:       stream.videoPackets + stream.audioPackets,
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

	defer func() {
		if error := recover(); error != nil {
			fmt.Println("Catching img panic: ", err)
		}
		fmt.Println("Cleaning up recentVideoPackets")
		stream.recentVideoPackets = make([]*rtp.Packet, 0)
	}()

	var data []byte
	if stream.lastKeyframe != nil {
		data = stream.lastKeyframe
	} else {
		samples := samplebuilder.New(100, &codecs.H264Packet{}, 90000)
		for _, packet := range stream.recentVideoPackets {
			samples.Push(packet)
		}

		sample := samples.Pop()
		if sample == nil {
			return nil
		}
		data = sample.Data
	}

	var img image.Image
	switch stream.videoCodec {
	case webrtc.MimeTypeH264:
		img, err = decodeH264Snapshot(data)
	}

	if err != nil {
		return err
	}
	if img == nil {
		fmt.Println("img is nil")
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

	fmt.Println("Got screenshot!")

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
		audioPackets:        0,
		videoPackets:        0,
		clientVendorName:    "",
		clientVendorVersion: "",
		recentVideoPackets:  make([]*rtp.Packet, 0),
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

func getPrimaryVideoTrack(tracks []StreamTrack) (StreamTrack, error) {
	for _, track := range tracks {
		if track.Type == webrtc.RTPCodecTypeVideo {
			return track, nil
		}
	}

	return StreamTrack{}, errors.New("no video tracks")
}

func decodeH264Snapshot(lastFullFrame []byte) (image.Image, error) {
	var img image.Image
	h264dec, err := h264.NewH264Decoder()
	if err != nil {
		return img, err
	}
	defer h264dec.Close()
	img, err = h264dec.Decode(lastFullFrame)
	if err != nil {
		return img, err
	}

	return img, nil
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
