package glimesh

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"strconv"

	"github.com/Glimesh/waveguide/pkg/control"
	"github.com/hasura/go-graphql-client"
	"github.com/sirupsen/logrus"
	"golang.org/x/oauth2/clientcredentials"
)

type Service struct {
	tokenUrl string
	apiUrl   string

	client     *graphql.Client
	httpClient *http.Client

	log logrus.FieldLogger

	Endpoint     string
	ClientID     string `mapstructure:"client_id"`
	ClientSecret string `mapstructure:"client_secret"`
}

func New(endpoint, clientID, clientSecret string) *Service {
	return &Service{
		tokenUrl: "/api/oauth/token",
		apiUrl:   "/api/graph",
	}
}

func (s *Service) SetLogger(log logrus.FieldLogger) {
	s.log = log
}

func (s *Service) Name() string {
	return "Glimesh"
}

func (s *Service) Connect() error {
	config := clientcredentials.Config{
		ClientID:     s.ClientID,
		ClientSecret: s.ClientSecret,
		TokenURL:     fmt.Sprintf("%s%s", s.Endpoint, s.tokenUrl),
		Scopes:       []string{"streamkey"},
	}
	s.httpClient = config.Client(context.Background())
	s.client = graphql.NewClient(fmt.Sprintf("%s%s", s.Endpoint, s.apiUrl), s.httpClient)

	return nil
}

func (s *Service) GetHmacKey(channelID control.ChannelID) ([]byte, error) {
	var hmacQuery struct {
		Channel struct {
			HmacKey graphql.String
		} `graphql:"channel(id: $id)"`
	}
	err := s.client.Query(context.Background(), &hmacQuery, map[string]interface{}{
		"id": graphql.ID(fmt.Sprint(channelID)),
	})
	if err != nil {
		return []byte{}, err
	}
	return []byte(hmacQuery.Channel.HmacKey), nil
}

func (s *Service) StartStream(channelID control.ChannelID) (control.StreamID, error) {
	var startStreamMutation struct {
		Stream struct {
			Id graphql.String
		} `graphql:"startStream(channelId: $id)"`
	}
	err := s.client.Mutate(context.Background(), &startStreamMutation, map[string]interface{}{
		"id": graphql.ID(fmt.Sprint(channelID)),
	})
	if err != nil {
		return 0, err
	}

	id, err := strconv.Atoi(string(startStreamMutation.Stream.Id))
	if err != nil {
		return 0, err
	}
	return control.StreamID(id), nil
}

func (s *Service) EndStream(streamID control.StreamID) error {
	var endStreamMutation struct {
		Stream struct {
			Id graphql.String
		} `graphql:"endStream(streamId: $id)"`
	}
	return s.client.Mutate(context.Background(), &endStreamMutation, map[string]interface{}{
		"id": graphql.ID(fmt.Sprint(streamID)),
	})
}

type StreamMetadataInput control.StreamMetadata

func (s *Service) UpdateStreamMetadata(streamID control.StreamID, metadata control.StreamMetadata) error {
	var logStreamMetadata struct {
		Stream struct {
			Id graphql.String
		} `graphql:"logStreamMetadata(streamId: $id, metadata: $metadata)"`
	}
	return s.client.Mutate(context.Background(), &logStreamMetadata, map[string]interface{}{
		"id": graphql.ID(fmt.Sprint(streamID)),
		"metadata": StreamMetadataInput{
			AudioCodec:        metadata.AudioCodec,
			IngestServer:      metadata.IngestServer,
			IngestViewers:     metadata.IngestViewers,
			LostPackets:       metadata.LostPackets,
			NackPackets:       metadata.NackPackets,
			RecvPackets:       metadata.RecvPackets,
			SourceBitrate:     metadata.SourceBitrate,
			SourcePing:        metadata.SourcePing,
			StreamTimeSeconds: metadata.StreamTimeSeconds,
			VendorName:        metadata.VendorName,
			VendorVersion:     metadata.VendorVersion,
			VideoCodec:        metadata.VideoCodec,
			VideoHeight:       metadata.VideoHeight,
			VideoWidth:        metadata.VideoWidth,
		},
	})
}

func (s *Service) SendJpegPreviewImage(streamID control.StreamID, img []byte) error {
	// Unfortunately hasura doesn't support this directly so we need to do a plain HTTP request
	query := `mutation {
		uploadStreamThumbnail(streamId: %d, thumbnail: "thumbdata") {
			id
		}
	}`

	return uploadThumbnail(s.httpClient, fmt.Sprintf("%s%s", s.Endpoint, s.apiUrl), fmt.Sprintf(query, streamID), img)
}

func uploadThumbnail(client *http.Client, url string, query string, image []byte) error {
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("thumbdata", "thumbnail.jpg")
	if err != nil {
		return err
	}
	part.Write(image)

	writer.WriteField("query", query)
	err = writer.Close()
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		return err
	}
	// Don't forget to set the content type, this will contain the boundary.
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Submit the request
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", res.Status)
	}

	return nil
}
