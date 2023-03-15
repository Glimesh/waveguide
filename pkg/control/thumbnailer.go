package control

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/pion/webrtc/v3"
)

// Note: This type of functionality will be common in Waveguide
// However we should not do it like this :D
func (s *Stream) thumbnailer(ctx context.Context, whepEndpoint string) error {
	log := s.log.WithField("app", "peersnap")

	log.Info("Started Thumbnailer")
	// Create a new PeerConnection
	peerConnection, err := webrtc.NewPeerConnection(webrtc.Configuration{}) //nolint exhaustive struct
	if err != nil {
		return err
	}
	defer peerConnection.Close()

	peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		kfer := NewKeyframer()
		codec := track.Codec()

		if codec.MimeType == "video/H264" {
			for {
				if ctx.Err() != nil {
					return
				}

				// Read RTP Packets in a loop
				p, _, readErr := track.ReadRTP()
				if readErr != nil {
					// Don't kill the thumbnailer after one weird RTP packet
					continue
				}

				keyframe := kfer.WriteRTP(p)
				if keyframe != nil {
					// fmt.Printf("!!! PEER KEYFRAME !!! %s\n\n", kfer)
					// saveImage(int(p.SequenceNumber), keyframe)
					// os.WriteFile(fmt.Sprintf("%d-peer.h264", p.SequenceNumber), keyframe, 0666)
					s.lastThumbnail <- keyframe
					kfer.Reset()
				}
			}
		}
	})

	url := fmt.Sprintf("%s/%d", whepEndpoint, s.ChannelID)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer([]byte{}))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/sdp")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if err := peerConnection.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  string(body),
	}); err != nil {
		return err
	}

	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		return err
	}

	// Create channel that is blocked until ICE Gathering is complete
	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)

	if err := peerConnection.SetLocalDescription(answer); err != nil {
		return err
	}

	// Block until ICE Gathering is complete, disabling trickle ICE
	// we do this because we only can exchange one signaling message
	// in a production application you should exchange ICE Candidates via OnICECandidate
	<-gatherComplete

	answerSdp := peerConnection.LocalDescription().SDP
	req2, err := http.NewRequest("POST", resp.Header.Get("location"), bytes.NewBufferString(answerSdp))
	if err != nil {
		return err
	}
	req2.Header.Set("Accept", "application/sdp")
	_, err = http.DefaultClient.Do(req2)
	if err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		log.Debug("received ctx done signal")
	case <-s.stopThumbnailer:
		log.Debug("received kill peersnap signal")
	}
	log.Info("ending thumbnailer")

	return nil
}
