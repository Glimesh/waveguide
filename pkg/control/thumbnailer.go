package control

import (
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media/h264writer"
)

type header struct {
	key   string
	value string
}

func (s *Stream) Ingest(ctx context.Context) error {
	logger := s.log.WithField("app", "ingest")
	done := make(chan struct{}, 1)
	go s.startIngestor(done)

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{}) //nolint exhaustive struct
	if err != nil {
		return err
	}
	defer pc.Close()

	pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		codec := track.Codec()

		if codec.MimeType == "video/H264" {
			for {
				if ctx.Err() != nil {
					return
				}

				p, _, readErr := track.ReadRTP()
				if readErr != nil {
					continue
				}

				select {
				case s.rtpIngest <- p:
				default:
				}
			}
		}
	})

	if err := s.setupPeerConnection(pc); err != nil {
		return err
	}

	<-ctx.Done()
	logger.Debug("received ctx done signal")
	done <- struct{}{}
	close(s.rtpIngest)

	return nil
}

func doHTTPRequest(uri, method string, body io.Reader, headers ...header) (*http.Response, error) {
	req, err := http.NewRequest(method, uri, body)
	if err != nil {
		return nil, err
	}

	for _, header := range headers {
		req.Header.Set(header.key, header.value)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

type Option struct {
	VideoWriter *h264writer.H264Writer
}

func (s *Stream) setupPeerConnection(pc *webrtc.PeerConnection) error {
	sdpHeader := header{"Accept", "application/sdp"}
	resp, err := doHTTPRequest(
		s.whepURI,
		http.MethodPost,
		strings.NewReader(""),
		sdpHeader,
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	offer, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if err := pc.SetRemoteDescription(
		webrtc.SessionDescription{
			Type: webrtc.SDPTypeOffer,
			SDP:  string(offer),
		}); err != nil {
		return err
	}

	answerSDP, err := pc.CreateAnswer(nil)
	if err != nil {
		return err
	}

	gatherComplete := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(answerSDP); err != nil {
		return err
	}
	<-gatherComplete

	answer := pc.LocalDescription().SDP
	_, err = doHTTPRequest( //nolint response is ignored
		resp.Header.Get("location"),
		http.MethodPost,
		strings.NewReader(answer),
		sdpHeader,
	)
	if err != nil {
		return err
	}

	return nil
}

func (s *Stream) startIngestor(done <-chan struct{}) {
LOOP:
	for {
		select {
		case p := <-s.rtpIngest:
			keyframe := s.keyframer.KeyFrame(p)
			if keyframe != nil {
				s.lastThumbnail <- keyframe
				s.keyframer.Reset()
			}
		case <-done:
			break LOOP
		}
	}
	s.log.Debug("ending rtp ingestor")
}
