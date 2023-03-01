package control

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	"github.com/pion/webrtc/v3"
	"github.com/sirupsen/logrus"
)

// Note: This type of functionality will be common in Waveguide
// However we should not do it like this :D
func peerSnapper(done <-chan bool, thumbnail chan<- []byte, whepEndpoint string, channelId ChannelID, log logrus.FieldLogger) {
	log.Info("Started Peersnap")
	// Create a new PeerConnection
	peerConnection, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		log.Error(err)
		return
	}

	peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		kfer := NewKeyframer()
		codec := track.Codec()

		if codec.MimeType == "video/H264" {
			for {
				// Read RTP Packets in a loop
				p, _, readErr := track.ReadRTP()
				if readErr != nil {
					log.Error(err)
					return
				}

				keyframe := kfer.WriteRTP(p)
				if keyframe != nil {
					// fmt.Printf("!!! PEER KEYFRAME !!! %s\n\n", kfer)
					// saveImage(int(p.SequenceNumber), keyframe)
					// os.WriteFile(fmt.Sprintf("%d-peer.h264", p.SequenceNumber), keyframe, 0666)
					thumbnail <- keyframe
					kfer.Reset()
				}
			}
		}

	})

	url := fmt.Sprintf("%s/%d", whepEndpoint, channelId)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer([]byte{}))
	if err != nil {
		log.Error(err)
		return
	}
	req.Header.Set("Accept", "application/sdp")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Error(err)
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Error(err)
		return
	}

	if err = peerConnection.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  string(body),
	}); err != nil {
		log.Error(err)
		return
	}

	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		log.Error(err)
		return
	}

	// Create channel that is blocked until ICE Gathering is complete
	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)

	if err = peerConnection.SetLocalDescription(answer); err != nil {
		log.Error(err)
		return
	}

	// Block until ICE Gathering is complete, disabling trickle ICE
	// we do this because we only can exchange one signaling message
	// in a production application you should exchange ICE Candidates via OnICECandidate
	<-gatherComplete

	answerSdp := peerConnection.LocalDescription().SDP
	req2, err := http.NewRequest("POST", resp.Header.Get("location"), bytes.NewBufferString(answerSdp))
	if err != nil {
		log.Error(err)
		return
	}
	req2.Header.Set("Accept", "application/sdp")
	_, err = http.DefaultClient.Do(req2)
	if err != nil {
		log.Error(err)
		return
	}

	<-done
	log.Info("Killing peersnap")
	peerConnection.Close()
}
