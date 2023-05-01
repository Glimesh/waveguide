package control

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/pion/webrtc/v3"
)

func (s *Stream) Ingest(ctx context.Context) error {
	logger := s.log.WithField("app", "ingest")
	done := make(chan struct{}, 1)

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{}) //nolint exhaustive struct
	if err != nil {
		return err
	}

	s.videoWriter = &noopFileWriter{}

	go s.startVideoIngestor()

	pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		cancelRead := make(chan struct{}, 1)
		go func() {
			<-done
			s.videoWriter.Done()
			s.log.Debug("exiting on track")
		LOOP:
			for {
				select {
				case <-s.lastThumbnail:
				default:
					s.log.Debug("thumbnail channel drained")
					break LOOP
				}
			}
			cancelRead <- struct{}{}
		}()

		codec := track.Codec()

		if trackCodec := codec.MimeType; trackCodec == webrtc.MimeTypeH264 {
			if s.saveVideo {
				filename := fmt.Sprintf("stream.%d.%s", s.StreamID, "out.h264")
				videoFileWriter := NewVideoWriter(
					s.log.WithField("file-writer", webrtc.MimeTypeH264),
					webrtc.MimeTypeH264,
					filename,
				)
				s.videoWriter = videoFileWriter

				go videoFileWriter.Run()
			}

			for {
				select {
				case <-cancelRead:
					s.log.Debug("on track stop signal")
					close(s.rtpIngest)
					return
				default:
					pkt, _, readErr := track.ReadRTP()
					if readErr != nil {
						// terminate the ingestor is input stream is EOF
						if errors.Is(readErr, io.EOF) {
							s.log.Debugf("read: %v", readErr)
							close(s.rtpIngest)
							return
						}
					}
					s.rtpIngest <- pkt
				}
			}
		}
	})

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

	if err := pc.SetRemoteDescription( //nolint shadow
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
	if err := pc.SetLocalDescription(answerSDP); err != nil { //nolint shadow
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

	<-ctx.Done()
	pc.Close()
	done <- struct{}{}
	logger.Debug("received ctx done signal")

	return nil
}

func (s *Stream) startVideoIngestor() {
	doneThumb := make(chan struct{}, 1)

	go s.thumbnailer(doneThumb)

	for p := range s.rtpIngest {
		select {
		case s.thumbnailReceiver <- p.Clone():
		default:
		}

		s.videoWriter.SendRTP(p)
	}
	s.log.Debug("closed ingestor listener")

	doneThumb <- struct{}{}
	s.log.Debug("ending rtp ingestor")
}

type header struct {
	key   string
	value string
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
