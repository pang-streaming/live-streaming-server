package httpsrv

import (
	"bytes"
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"liveflow/config"
	"net/http"
	"net/http/httptest"
	"path"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/bluenviron/gohlslib/pkg/codecparams"
	"github.com/bluenviron/gohlslib/pkg/playlist"
	"github.com/labstack/echo/v4"

	"liveflow/log"
	"liveflow/media/hlshub"
)

type Handler struct {
	endpoint *hlshub.HLSHub
	s3Client *s3.Client
	config   config.Config
}

func NewHandler(hlsEndpoint *hlshub.HLSHub, conf config.Config) *Handler {
	return &Handler{
		endpoint: hlsEndpoint,
		s3Client: s3.NewFromConfig(getAWSConfig(conf)),
		config:   conf,
	}
}

func (h *Handler) HandleMasterM3U8(c echo.Context) error {
	ctx := context.Background()
	log.Info(ctx, "HandleMasterM3U8")
	workID := c.Param("streamID")
	muxers, err := h.endpoint.MuxersByWorkID(workID)
	if err != nil {
		log.Error(ctx, err, "get muxer failed")
		return fmt.Errorf("get muxer failed: %w", err)
	}
	m3u8Version := 3
	pl := &playlist.Multivariant{
		Version: func() int {
			return m3u8Version
		}(),
		IndependentSegments: true,
	}
	var variants []*playlist.MultivariantVariant
	for name, muxer := range muxers {
		average := 33033
		variant := &playlist.MultivariantVariant{
			Bandwidth: average,
			FrameRate: nil,
			URI:       path.Join(name, "stream.m3u8"),
		}
		variant.Codecs = []string{}
		if muxer.VideoTrack != nil {
			variant.Codecs = append(variant.Codecs, codecparams.Marshal(muxer.VideoTrack.Codec))
		}
		if muxer.AudioTrack != nil {
			variant.Codecs = append(variant.Codecs, codecparams.Marshal(muxer.AudioTrack.Codec))
		}
		variants = append(variants, variant)
	}
	pl.Variants = variants
	c.Response().Header().Set(h.config.S3.CacheControl, "max-age=1")
	masterM3u8Bytes, err := pl.Marshal()
	if err != nil {
		return err
	}

	tmpDir := filepath.Join("hls/", workID)
	masterFilePath := filepath.Join(tmpDir, "master.m3u8")

	log.Info(ctx, "master.m3u8 saved to tmp:", h.config.S3.Access)

	_, err = h.s3Client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket: aws.String("pang-streaming-dev-bucket"),
		Key:    aws.String(masterFilePath),
		Body:   bytes.NewReader(masterM3u8Bytes),
	})

	return c.Blob(http.StatusOK, "application/vnd.apple.mpegurl", masterM3u8Bytes)
}

func (h *Handler) HandleM3U8(c echo.Context) error {
	ctx := context.Background()
	log.Info(ctx, "HandleM3U8")
	workID := c.Param("streamID")
	playlistName := c.Param("playlistName")
	muxer, err := h.endpoint.Muxer(workID, playlistName)
	if err != nil {
		log.Error(ctx, err, "no hls stream")
		return c.NoContent(http.StatusNotFound)
	}

	recorder := httptest.NewRecorder()
	muxer.Handle(recorder, c.Request())
	data := recorder.Body.Bytes()
	fileName := filepath.Base(c.Request().URL.Path)
	tmpDir := filepath.Join("hls/", workID, playlistName)
	filePath := filepath.Join(tmpDir, fileName)
	_, err = h.s3Client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket: aws.String("pang-streaming-dev-bucket"),
		Key:    aws.String(filePath),
		Body:   bytes.NewReader(data),
	})

	extension := filepath.Ext(c.Request().URL.String())
	switch extension {
	case ".m3u8":
		c.Response().Header().Set(h.config.S3.CacheControl, "max-age=1")
	case ".ts", ".mp4":
		c.Response().Header().Set(h.config.S3.CacheControl, "max-age=3600")
	}
	muxer.Handle(c.Response(), c.Request())
	return nil
}

func getAppCredentials(conf config.Config) aws.CredentialsProvider {
	return credentials.NewStaticCredentialsProvider(
		conf.S3.Access,
		conf.S3.Secret,
		"",
	)
}

func getAWSConfig(config config.Config) aws.Config {
	return aws.Config{
		Credentials: getAppCredentials(config),
		Region:      config.S3.Region,
	}
}
