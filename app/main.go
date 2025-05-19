package main

import (
	"context"
	"fmt"
	"live-streaming-server/internal/handler"
	"live-streaming-server/internal/logger"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"live-streaming-server/config"
	"live-streaming-server/media/hlshub"
	"live-streaming-server/media/hub"
	"live-streaming-server/media/streamer/egress/hls"
	"live-streaming-server/media/streamer/egress/record/mp4"
	"live-streaming-server/media/streamer/egress/record/webm"
	"live-streaming-server/media/streamer/egress/whep"
	"live-streaming-server/media/streamer/ingress/rtmp"
	"live-streaming-server/media/streamer/ingress/whip"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/pion/webrtc/v3"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

func main() {
	// 설정 로드
	conf, err := loadConfig()
	if err != nil {
		panic(fmt.Errorf("설정 로드 실패: %w", err))
	}

	// 로깅 초기화
	logger.Init()

	// 컨텍스트 및 시그널 핸들링 설정
	ctx, _ := setupContextWithSignalHandling()
	ctx = logger.WithFields(ctx, logrus.Fields{"app": "live-streaming-server"})
	logger.Info(ctx, "live-streaming-server가 시작되었습니다")

	// 미디어 허브 초기화
	hub := hub.NewHub()
	tracks := make(map[string][]*webrtc.TrackLocalStaticRTP)

	// API 서버 및 미디어 서비스 시작
	go startAPIServer(ctx, hub, tracks, conf)

	// RTMP 서버 시작
	rtmpServer := rtmp.NewRTMP(rtmp.RTMPArgs{
		Hub:  hub,
		Port: conf.RTMP.Port,
	})
	rtmpServer.Serve(ctx)
}

// 설정을 로드하는 함수
func loadConfig() (config.Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("toml")
	viper.AddConfigPath(".")
	viper.BindEnv("docker.mode", "DOCKER_MODE")

	err := viper.ReadInConfig()
	if err != nil {
		return config.Config{}, fmt.Errorf("설정 파일 읽기 오류: %w", err)
	}

	var conf config.Config
	err = viper.Unmarshal(&conf)
	if err != nil {
		return config.Config{}, fmt.Errorf("설정 언마샬링 오류: %w", err)
	}

	fmt.Printf("설정: %+v\n", conf)
	return conf, nil
}

// 컨텍스트와 시그널 핸들링을 설정
func setupContextWithSignalHandling() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigs
		logger.Info(ctx, "신호 수신:", sig)
		logger.Info(ctx, "종료 시작...")
		cancel()
	}()

	return ctx, cancel
}

// API 서버 설정 및 시작
func startAPIServer(ctx context.Context, mediaHub *hub.Hub, tracks map[string][]*webrtc.TrackLocalStaticRTP, conf config.Config) {
	api := setupEchoServer()
	hlsHub := hlshub.NewHLSHub()

	// HLS 라우팅 설정
	setupHLSRoutes(api, hlsHub)

	// 모니터링 엔드포인트 설정
	setupMonitoringEndpoints(api)

	// WHIP 서버 설정
	whipServer := whip.NewWHIP(whip.WHIPArgs{
		Hub:        mediaHub,
		Tracks:     tracks,
		DockerMode: conf.Docker.Mode,
		Echo:       api,
	})
	whipServer.RegisterRoute()

	// API 서버 시작
	go startAPIListener(api, conf.Service.Port)

	// 스트림 처리 서비스 시작
	processIncomingStreams(ctx, mediaHub, hlsHub, tracks, conf)
}

// Echo 서버 초기화
func setupEchoServer() *echo.Echo {
	api := echo.New()
	api.HideBanner = true
	return api
}

// HLS 라우팅 설정
func setupHLSRoutes(api *echo.Echo, hlsHub *hlshub.HLSHub) {
	hlsHandler := handler.NewHandler(hlsHub)
	hlsRoute := api.Group("/hls", middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{http.MethodGet, http.MethodHead, http.MethodOptions},
	}))

	hlsRoute.GET("/:streamID/master.m3u8", hlsHandler.HandleMasterM3U8)
	hlsRoute.GET("/:streamID/:playlistName/stream.m3u8", hlsHandler.HandleM3U8)
	hlsRoute.GET("/:streamID/:playlistName/:resourceName", hlsHandler.HandleM3U8)
}

// 모니터링 엔드포인트 설정
func setupMonitoringEndpoints(api *echo.Echo) {
	api.GET("/prometheus", echo.WrapHandler(promhttp.Handler()))
	api.GET("/debug/pprof/*", echo.WrapHandler(http.DefaultServeMux))
}

// API 리스너 시작
func startAPIListener(api *echo.Echo, port int) {
	listenAddr := "0.0.0.0:" + strconv.Itoa(port)
	fmt.Println("API 서버 시작 -", listenAddr)
	if err := api.Start(listenAddr); err != nil && err != http.ErrServerClosed {
		fmt.Printf("API 서버 시작 실패: %v\n", err)
	}
}

// 스트림 처리
func processIncomingStreams(ctx context.Context, mediaHub *hub.Hub, hlsHub *hlshub.HLSHub,
	tracks map[string][]*webrtc.TrackLocalStaticRTP, conf config.Config) {
	for source := range mediaHub.SubscribeToStreamID() {
		logger.Infof(ctx, "새 스트림 ID 수신: %s", source.StreamID())

		// MP4 레코딩 시작
		if conf.MP4.Record {
			startMP4Recording(ctx, mediaHub, source)
		}

		// WebM 레코딩 시작
		if conf.EBML.Record {
			startWebMRecording(ctx, mediaHub, source)
		}

		// HLS 서비스 시작
		startHLSService(ctx, mediaHub, hlsHub, conf, source)

		// WHEP 서비스 시작
		startWHEPService(ctx, mediaHub, tracks, source)
	}
}

// MP4 레코딩 시작
func startMP4Recording(ctx context.Context, mediaHub *hub.Hub, source hub.Source) {
	mp4Recorder := mp4.NewMP4(mp4.MP4Args{
		Hub:             mediaHub,
		SplitIntervalMS: 3000,
	})

	if err := mp4Recorder.Start(ctx, source); err != nil {
		logger.Errorf(ctx, "MP4 시작 실패: %v", err)
	}
}

// WebM 레코딩 시작
func startWebMRecording(ctx context.Context, mediaHub *hub.Hub, source hub.Source) {
	webmRecorder := webm.NewWEBM(webm.WebMArgs{
		Hub:             mediaHub,
		SplitIntervalMS: 6000,
		StreamID:        source.StreamID(),
	})

	if err := webmRecorder.Start(ctx, source); err != nil {
		logger.Errorf(ctx, "WebM 시작 실패: %v", err)
	}
}

// HLS 서비스 시작
func startHLSService(ctx context.Context, mediaHub *hub.Hub, hlsHub *hlshub.HLSHub,
	conf config.Config, source hub.Source) {
	hlsService := hls.NewHLS(hls.HLSArgs{
		Hub:     mediaHub,
		HLSHub:  hlsHub,
		Port:    conf.Service.Port,
		LLHLS:   conf.Service.LLHLS,
		DiskRam: conf.Service.DiskRam,
	})

	if err := hlsService.Start(ctx, source); err != nil {
		logger.Errorf(ctx, "HLS 시작 실패: %v", err)
	}
}

// WHEP 서비스 시작
func startWHEPService(ctx context.Context, mediaHub *hub.Hub, tracks map[string][]*webrtc.TrackLocalStaticRTP,
	source hub.Source) {
	whepService := whep.NewWHEP(whep.WHEPArgs{
		Tracks: tracks,
		Hub:    mediaHub,
	})

	if err := whepService.Start(ctx, source); err != nil {
		logger.Errorf(ctx, "WHEP 시작 실패: %v", err)
	}
}
