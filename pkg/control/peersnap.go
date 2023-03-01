package control

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media/samplebuilder"
)

func NewPeersnap() {

}

type Peersnap struct {
	Sampler *samplebuilder.SampleBuilder
}

func (ps *Peersnap) Start(channelId ChannelID) {
	ps.Sampler = samplebuilder.New(200, &codecs.H264Packet{}, 90000)

	fmt.Println("Started Peersnap")
	// Create a new PeerConnection
	peerConnection, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		panic(err)
	}

	// Set a handler for when a new remote track starts, this handler saves buffers to SampleBuilder
	// so we can generate a snapshot
	peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		// Send a PLI on an interval so that the publisher is pushing a keyframe every rtcpPLIInterval
		go func() {
			ticker := time.NewTicker(time.Second * 3)
			for range ticker.C {
				errSend := peerConnection.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(track.SSRC())}})
				if errSend != nil {
					fmt.Println(errSend)
				}
			}
		}()

		// fmt.Println(track)

		kfer := NewKeyframer()
		codec := track.Codec()
		for {
			// Read RTP Packets in a loop
			p, _, readErr := track.ReadRTP()
			if readErr != nil {
				panic(readErr)
			}

			if codec.MimeType == "video/H264" {
				keyframe := kfer.WriteRTP(p)
				if keyframe != nil {
					fmt.Printf("!!! PEER KEYFRAME !!! %s\n\n", kfer)
					saveImage(int(p.SequenceNumber), keyframe)
					os.WriteFile(fmt.Sprintf("%d-peer.h264", p.SequenceNumber), keyframe, 0666)
					kfer.Reset()
				}
			}

		}
	})

	// Set the handler for ICE connection state
	// This will notify you when the peer has connected/disconnected
	peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		fmt.Printf("ICE Connection State has changed: %s\n", connectionState.String())
	})

	url := fmt.Sprintf("http://localhost:8091/whep/endpoint/%d", channelId)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer([]byte{}))
	if err != nil {
		panic(err)
	}
	req.Header.Set("Accept", "application/sdp")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}

	if err = peerConnection.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  string(body),
	}); err != nil {
		panic(err)
	}

	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		panic(err)
	}

	// Create channel that is blocked until ICE Gathering is complete
	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)

	if err = peerConnection.SetLocalDescription(answer); err != nil {
		panic(err)
	}

	// Block until ICE Gathering is complete, disabling trickle ICE
	// we do this because we only can exchange one signaling message
	// in a production application you should exchange ICE Candidates via OnICECandidate
	<-gatherComplete

	answerSdp := peerConnection.LocalDescription().SDP
	req2, err := http.NewRequest("POST", resp.Header.Get("location"), bytes.NewBufferString(answerSdp))
	if err != nil {
		panic(err)
	}
	req2.Header.Set("Accept", "application/sdp")
	_, err = http.DefaultClient.Do(req2)
	if err != nil {
		panic(err)
	}

	// go ps.Snapshotter()

	select {}
}

func (ps *Peersnap) Snapshotter() {
	ticker := time.NewTicker(5 * time.Second)
	go func() {
		sampleBuilder := samplebuilder.New(20, &codecs.H264Packet{}, 90000)
		done := make(chan bool)

		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				fmt.Println("Snapshotter tick")

				sample := sampleBuilder.Pop()
				if sample == nil {
					fmt.Println("Sample not ready")
					continue
				}

				saveImage(999, sample.Data)
			}
		}
	}()
}
