package control

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Glimesh/waveguide/pkg/disk"

	"github.com/livekit/server-sdk-go/pkg/samplebuilder"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
	"github.com/pion/webrtc/v3/pkg/media/oggwriter"
)

func (s *Stream) Ingest(ctx context.Context) error {
	logger := s.log.WithField("app", "ingest")
	doneVideo := make(chan struct{}, 1)
	doneAudio := make(chan struct{}, 1)

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{}) //nolint exhaustive struct
	if err != nil {
		return err
	}

	go s.startVideoIngestor()

	// TODO: refactor the following OnTrack callback logic
	pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		codec := track.Codec().MimeType
		kind := track.Kind()

		// only h264 codec is supported for now, hence the check below
		// this will probably have to go away once support for other codecs is added

		// refactoring idea for future me
		// one approach is to construct ingestor structs for audio and video media
		// the respective ingestor holds the reference to the remote track
		// and the appropriate sub-consumers e.g. the video ingestor holds the thumbnailer
		// and the file-writer while the audio ingestor holds just the file writer
		//
		// the respective ingestor then holds the logic for multiplexing/writing
		// the rtp packets from the remote track to its consumers
		if trackCodec := codec; trackCodec == webrtc.MimeTypeH264 {
			s.log.Debug("got h264 track")
			cancelRead := s.cancelVideoRead(doneVideo)
			if s.saveVideo {
				err := s.initFileWriter(codec, kind) //nolint no shadow
				if err != nil {
					s.log.Debugf("failed to init video file writer: %v", err)
				}
				go s.videoWriter.Run()
			}

			for {
				select {
				case <-cancelRead:
					s.log.Debug("on video track stop signal")
					close(s.videoRTPIngest)
					return
				default:
				}
				pkt, _, readErr := track.ReadRTP()
				if readErr != nil && errors.Is(readErr, io.EOF) {
					// terminate the ingestor is input stream is EOF
					s.log.Debugf("read: %v", readErr)
					close(s.videoRTPIngest)
					return
				}
				s.videoRTPIngest <- pkt
			}
		} else if trackCodec == webrtc.MimeTypeOpus {
			s.log.Debug("got opus track")
			if s.saveAudio {
				cancelRead := s.cancelAudioRead(doneAudio)
				s.log.Debug("audio file writer enabled")
				sb := samplebuilder.New(10, &codecs.OpusPacket{}, track.Codec().ClockRate)
				writer, err := oggwriter.New(s.StreamID.String()+".ogg", 48000, track.Codec().Channels)
				if err != nil {
					return
				}
				t := &TrackWriter{
					writer: writer,
					sb:     sb,
					track:  track,
				}
				go t.start(cancelRead)
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
	logger.Debug("received ctx done signal")
	pc.Close()
	doneVideo <- struct{}{}
	doneAudio <- struct{}{}

	return nil
}

type TrackWriter struct {
	sb     *samplebuilder.SampleBuilder
	writer media.Writer
	track  *webrtc.TrackRemote
}

func (t *TrackWriter) start(cancel chan struct{}) {
	defer t.writer.Close()
	for {
		select {
		case <-cancel:
			return
		default:
		}
		pkt, _, err := t.track.ReadRTP()
		if err != nil {
			break
		}
		t.sb.Push(pkt)

		for _, p := range t.sb.PopPackets() {
			t.writer.WriteRTP(p)
		}
	}
}

func (s *Stream) cancelAudioRead(done chan struct{}) chan struct{} {
	cancel := make(chan struct{}, 1)
	go func() {
		s.log.Debug("starting cancel audio read loop")
		<-done
		// cancel the read loop and stop the ingestion
		cancel <- struct{}{}
		s.log.Debug("exiting cancel audio read loop")
	}()
	return cancel
}

func (s *Stream) cancelVideoRead(done chan struct{}) chan struct{} {
	cancel := make(chan struct{}, 1)
	go func() {
		s.log.Debug("starting cancel video read loop")
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
		s.log.Debug("exiting cancel video read loop")
		cancel <- struct{}{}
	}()
	return cancel
}

// initFileWriter initializes the file writer and sets it for the
// stream based on the codec mime and type
func (s *Stream) initFileWriter(mime string, kind webrtc.RTPCodecType) error {
	var (
		writer   disk.Writer
		err      error
		streamID = fmt.Sprintf("%d", s.StreamID)
		fw       = &fileWriter{ //nolint
			log:      s.log.WithField("file-writer", mime),
			packetCh: make(chan *rtp.Packet, 100),
			done:     make(chan struct{}, 1),
		}
	)

	switch kind {
	case webrtc.RTPCodecTypeVideo:
		writer, err = disk.NewVideoWriter(mime, streamID)
		if err != nil {
			return err
		}
		fw.writer = writer
		s.videoWriter = fw
		return nil

		// not needed?
	case webrtc.RTPCodecTypeAudio:

	default:
		s.log.Panicf("unknown codec type: %v", kind)
	}
	return nil
}

func (s *Stream) startVideoIngestor() {
	doneThumb := make(chan struct{}, 1)
	go s.thumbnailer(doneThumb)

	for p := range s.videoRTPIngest {
		select {
		case s.thumbnailReceiver <- p.Clone():
		default:
		}

		s.videoWriter.SendRTP(p)
	}

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
