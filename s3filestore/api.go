package s3filestore

import (
	"context"
	"fmt"
	"time"

	"github.com/acdifran/go-tools/common"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
)

type PresignConfig struct {
	cacheDuration      time.Duration
	expirationDuration time.Duration
}

type PresignOption func(*PresignConfig)

func SetCacheDuration(duration time.Duration) PresignOption {
	return func(c *PresignConfig) {
		c.cacheDuration = duration
	}
}

func SetExpirationDuration(duration time.Duration) PresignOption {
	return func(c *PresignConfig) {
		c.cacheDuration = duration
	}
}

func processOptions(opts ...PresignOption) *PresignConfig {
	config := &PresignConfig{
		cacheDuration:      time.Hour,
		expirationDuration: time.Minute * 10,
	}

	for _, opt := range opts {
		opt(config)
	}

	return config
}

func (s *S3FileStore) PresignedGetUrl(key string, opts ...PresignOption) (string, error) {
	getObjectParams := &s3.GetObjectInput{
		Bucket: aws.String(s.BucketName),
		Key:    aws.String(key),
	}

	config := processOptions(opts...)
	presigner := s3.NewPresignClient(s.Client)
	resp, err := presigner.PresignGetObject(context.TODO(), getObjectParams,
		s3.WithPresignExpires(config.expirationDuration),
	)
	if err != nil {
		return "", fmt.Errorf("signing request, %w", err)
	}

	return resp.URL, nil
}

func (s *S3FileStore) CreatePresignedPutUrl(
	opts ...PresignOption,
) (*common.PresignedUrlObj, error) {
	config := processOptions(opts...)
	return s.createPresignedUrl("", config.cacheDuration, config.expirationDuration)
}

func (s *S3FileStore) PresignedPutUrl(key string, opts ...PresignOption) (string, error) {
	config := processOptions(opts...)
	urlObj, err := s.createPresignedUrl(key, config.cacheDuration, config.expirationDuration)
	if err != nil {
		return "", fmt.Errorf("creating presigned url: %w", err)
	}
	return urlObj.URL, nil
}

func (s *S3FileStore) createPresignedUrl(
	key string,
	cacheDuration time.Duration,
	expiration time.Duration,
) (*common.PresignedUrlObj, error) {
	if key == "" {
		key = uuid.New().String()
	}

	putObjectParams := &s3.PutObjectInput{
		Bucket: aws.String(s.BucketName),
		Key:    aws.String(key),
		CacheControl: aws.String(
			fmt.Sprintf("max-age=%d, must-revalidate", int(cacheDuration.Seconds())),
		),
	}

	presigner := s3.NewPresignClient(s.Client)
	resp, err := presigner.PresignPutObject(context.TODO(), putObjectParams,
		s3.WithPresignExpires(expiration),
	)
	if err != nil {
		return nil, fmt.Errorf("signing request, %w", err)
	}

	return &common.PresignedUrlObj{Key: key, URL: resp.URL}, nil
}
