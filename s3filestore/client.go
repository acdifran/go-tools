package s3filestore

import (
	"context"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3FileStore struct {
	Client                           *s3.Client
	BucketName                       string
	DefaultObjectCacheDuration       *time.Duration
	DefaultPresignExpirationDuration time.Duration
}

func NewS3FileStore(bucketName string, region string) *S3FileStore {
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(region))
	if err != nil {
		log.Fatalf("Error initializing AWS Config: %v", err)
	}

	s3Client := s3.NewFromConfig(cfg)

	return &S3FileStore{Client: s3Client, BucketName: bucketName}
}
