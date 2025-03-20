package s3filestore

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type PresignConfig struct {
	cacheDuration      *time.Duration
	expirationDuration time.Duration
}

type PresignOption func(*PresignConfig)

func SetCacheDuration(duration time.Duration) PresignOption {
	return func(c *PresignConfig) {
		c.cacheDuration = &duration
	}
}

func SetExpirationDuration(duration time.Duration) PresignOption {
	return func(c *PresignConfig) {
		c.expirationDuration = duration
	}
}

func (s *S3FileStore) processOptions(opts ...PresignOption) *PresignConfig {
	config := &PresignConfig{
		cacheDuration:      s.DefaultObjectCacheDuration,
		expirationDuration: s.DefaultPresignExpirationDuration,
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

	config := s.processOptions(opts...)
	presigner := s3.NewPresignClient(s.Client)
	resp, err := presigner.PresignGetObject(context.TODO(), getObjectParams,
		s3.WithPresignExpires(config.expirationDuration),
	)
	if err != nil {
		return "", fmt.Errorf("signing request, %w", err)
	}

	return resp.URL, nil
}

func (s *S3FileStore) PresignedPutUrl(
	key string,
	opts ...PresignOption,
) (string, error) {
	config := s.processOptions(opts...)

	var cacheControl *string
	if config.cacheDuration != nil {
		cacheControl = aws.String(
			fmt.Sprintf("max-age=%d, must-revalidate", int(config.cacheDuration.Seconds())),
		)
	}

	putObjectParams := &s3.PutObjectInput{
		Bucket:       aws.String(s.BucketName),
		Key:          aws.String(key),
		CacheControl: cacheControl,
	}

	presigner := s3.NewPresignClient(s.Client)
	resp, err := presigner.PresignPutObject(context.TODO(), putObjectParams,
		s3.WithPresignExpires(config.expirationDuration),
	)
	if err != nil {
		return "", fmt.Errorf("signing request, %w", err)
	}

	return resp.URL, nil
}
