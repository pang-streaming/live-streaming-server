package httpsrv

import (
	"bytes"
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
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

const (
	cacheControl = "CDN-Cache-Control"
	// 임시 테스트용
	AWS_ACCESS_KEY = ""
	AWS_SECRET_KEY = ""
	AWS_REGION     = "ap-northeast-2"
)

type Handler struct {
	endpoint *hlshub.HLSHub
	s3Client *s3.Client
}

func NewHandler(hlsEndpoint *hlshub.HLSHub) *Handler {
	return &Handler{
		endpoint: hlsEndpoint,
		s3Client: s3.NewFromConfig(getAWSConfig()),
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
		// TODO: muxer.Bandwidth() is not implemented
		//_, average, err := muxer.Bandwidth()
		//if err != nil {
		//	continue
		//}
		average := 33033
		variant := &playlist.MultivariantVariant{
			Bandwidth: average,
			FrameRate: nil,
			URI:       path.Join(name, "stream.m3u8"),
		}
		// TODO: muxer.ResolutionString() is not implemented
		//resolution, err := muxer.ResolutionString()
		//if err == nil {
		//	variant.Resolution = resolution
		//}
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
	c.Response().Header().Set(cacheControl, "max-age=1")
	masterM3u8Bytes, err := pl.Marshal()
	if err != nil {
		return err
	}

	// 임시 테스트용
	tmpDir := filepath.Join("hls/", workID)
	masterFilePath := filepath.Join(tmpDir, "master.m3u8")
	_, err = h.s3Client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket: aws.String("pang-streaming-dev-bucket"),
		Key:    aws.String(masterFilePath),
		Body:   bytes.NewReader(masterM3u8Bytes),
	})

	//if err := os.MkdirAll(tmpDir, 0755); err != nil {
	//	log.Error(ctx, err, "failed to create tmp directory")
	//} else {
	//	masterFilePath := filepath.Join(tmpDir, "master.m3u8")
	//	err = os.WriteFile(masterFilePath, masterM3u8Bytes, 0644)
	//	if err != nil {
	//		log.Error(ctx, err, "failed to write master.m3u8")
	//	} else {
	//		log.Info(ctx, "master.m3u8 saved to tmp:", masterFilePath)
	//	}
	//
	//}

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

	//if err := os.MkdirAll(tmpDir, 0755); err != nil {
	//	log.Error(ctx, err, "failed to create tmp directory")
	//} else {
	//	fileName := filepath.Base(c.Request().URL.Path)
	//	filePath := filepath.Join(tmpDir, fileName)
	//	if err := os.WriteFile(filePath, data, 0644); err != nil {
	//		log.Error(ctx, err, "failed to write file", filePath)
	//	} else {
	//		log.Info(ctx, "file saved to tmp:", filePath)
	//	}
	//}

	extension := filepath.Ext(c.Request().URL.String())
	switch extension {
	case ".m3u8":
		c.Response().Header().Set(cacheControl, "max-age=1")
	case ".ts", ".mp4":
		c.Response().Header().Set(cacheControl, "max-age=3600")
	}
	muxer.Handle(c.Response(), c.Request())
	return nil
}

// 임시 테스트용
func getAppCredentials() aws.CredentialsProvider {
	return credentials.NewStaticCredentialsProvider(
		AWS_ACCESS_KEY,
		AWS_SECRET_KEY,
		"",
	)
}

// AWS Config를 반환하는 함수
func getAWSConfig() aws.Config {
	return aws.Config{
		Credentials: getAppCredentials(),
		Region:      AWS_REGION,
	}
}
