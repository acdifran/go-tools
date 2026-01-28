package gcloudfilestore

import (
	"context"
	"log"
	"time"

	"cloud.google.com/go/storage"
)

type GCloudFileStore struct {
	Client                           *storage.Client
	BucketName                       string
	DefaultObjectCacheDuration       *time.Duration
	DefaultPresignExpirationDuration time.Duration
}

func New(bucketName string) *GCloudFileStore {
	ctx := context.Background()
	client, err := storage.NewClient(ctx)
	if err != nil {
		log.Fatalf("Error initializing Google Cloud Storage client: %v", err)
	}

	return &GCloudFileStore{Client: client, BucketName: bucketName}
}