package control

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Glimesh/waveguide/pkg/disk"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3"
)

func (s *Stream) Ingest(ctx context.Context) error {
	logger := s.log.WithField("app", "ingest")
	done := make(chan struct{}, 1)

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{}) //nolint exhaustive struct
	if err != nil {
		return err
	}

	go s.startVideoIngestor()

	pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		cancelRead := make(chan struct{}, 1)
		go func() {
			// this goroutine spawns each time for audio & video track
			// for now it leaking as there's no way to cancel it
			// for the audio track
			// TODO: fix goroutine leak for audio track
			s.log.Debug("starting cancel read loop")
			<-done
			s.videoWriter.Done()
		LOOP:
			for {
				select {
				// drain the thumbnail channel on exit
				case <-s.lastThumbnail:
				default:
					s.log.Debug("thumbnail channel drained")
					break LOOP
				}
			}
			// cancel the read loop and stop the ingestion
			s.log.Debug("exiting cancel read loop")
			cancelRead <- struct{}{}
		}()

		codec := track.Codec().MimeType
		kind := track.Kind()

		// only h264 codec is supported for now, hence the check below
		// this will probably have to go away once support for other codecs is added
		if trackCodec := codec; trackCodec == webrtc.MimeTypeH264 {
			s.log.Debug("got h264 track")
			writer, err := s.initFileWriter(codec, kind) //nolint no shadow
			if err != nil {
				s.log.Debugf("failed to init video file writer: %v", err)
			}
			s.videoWriter = writer
			go s.videoWriter.Run()

			for {
				select {
				case <-cancelRead:
					s.log.Debug("on track stop signal")
					close(s.rtpIngest)
					return
				default:
				}
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
		} else if trackCodec == webrtc.MimeTypeOpus {
			// TODO: implement opus writer
			s.log.Debug("got opus track")
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

func (s *Stream) initFileWriter(mime string, kind webrtc.RTPCodecType) (FileWriter, error) {
	var (
		writer   disk.Writer
		err      error
		streamID = fmt.Sprintf("%d", s.StreamID)
	)
	switch kind {
	case webrtc.RTPCodecTypeVideo:
		if s.saveVideo {
			writer, err = disk.NewVideoWriter(mime, streamID)
		}
	case webrtc.RTPCodecTypeAudio:
		if s.saveAudio {
			writer, err = disk.NewAudioWriter(mime, streamID)
		}
	}
	if err != nil {
		return nil, err
	}

	return &fileWriter{
		writer:   writer,
		log:      s.log.WithField("file-writer", mime),
		packetCh: make(chan *rtp.Packet, 100),
		done:     make(chan struct{}, 1),
	}, nil
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
