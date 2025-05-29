package redis

import (
	"context"
	"errors"
	"fmt"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"liveflow/config"
	"liveflow/log"
	"time"
)

var (
	ErrRedisDisabled = errors.New("redis is disabled")

	ErrStreamKeyNotFound = errors.New("stream key not found")

	ErrInvalidStreamKey = errors.New("invalid stream key")
)

type Client struct {
	client *redis.Client
	Config *config.Config
}

func NewClient(config *config.Config) (*Client, error) {
	if !config.Redis.Enabled {
		return &Client{Config: config}, nil
	}

	client := redis.NewClient(&redis.Options{
		Addr:     config.Redis.Host + ":" + config.Redis.Port,
		Password: config.Redis.Password,
		DB:       config.Redis.DB,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.Ping(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("redis connection failed: %w", err)
	}

	return &Client{
		client: client,
		Config: config,
	}, nil
}

func (c *Client) IsEnabled() bool {
	return c.Config.Redis.Enabled
}

func (c *Client) Close() error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

func (c *Client) GetStreamerIDByKey(ctx context.Context, streamKey string) (string, error) {
	if !c.IsEnabled() {
		return "", ErrRedisDisabled
	}

	ctx = log.WithFields(ctx, logrus.Fields{
		"streamKey": streamKey,
		"action":    "get_streamer_id",
	})

	// Redis에서 스트림 키로 스트리머 ID 조회
	key := fmt.Sprintf("stream_key:%s", streamKey)
	streamerID, err := c.client.Get(ctx, key).Result()
	if err == redis.Nil {
		log.Warn(ctx, "Stream key not found in Redis")
		return "", ErrStreamKeyNotFound
	} else if err != nil {
		log.Error(ctx, "Redis error:", err)
		return "", err
	}

	log.Info(ctx, "Found streamer ID for key:", streamerID)
	return streamerID, nil
}

func (c *Client) ValidateStreamKey(ctx context.Context, streamKey string) error {
	if !c.IsEnabled() {
		return ErrRedisDisabled
	}

	ctx = log.WithFields(ctx, logrus.Fields{
		"streamKey": streamKey,
		"action":    "validate_stream_key",
	})

	// Redis에서 스트림 키 존재 여부 확인
	key := fmt.Sprintf("stream_key:%s", streamKey)
	exists, err := c.client.Exists(ctx, key).Result()
	if err != nil {
		log.Error(ctx, "Redis error:", err)
		return err
	}

	if exists == 0 {
		log.Warn(ctx, "Stream key not found in Redis")
		return ErrStreamKeyNotFound
	}

	log.Info(ctx, "Stream key validated successfully")
	return nil
}
