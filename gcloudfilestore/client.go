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
	ServiceAccount                   string
	DefaultObjectCacheDuration       *time.Duration
	DefaultPresignExpirationDuration time.Duration
}

type Option func(*GCloudFileStore)

func WithServiceAccount(email string) Option {
	return func(g *GCloudFileStore) {
		g.ServiceAccount = email
	}
}

func New(bucketName string, opts ...Option) *GCloudFileStore {
	ctx := context.Background()
	client, err := storage.NewClient(ctx)
	if err != nil {
		log.Fatalf("Error initializing Google Cloud Storage client: %v", err)
	}

	g := &GCloudFileStore{Client: client, BucketName: bucketName}
	for _, opt := range opts {
		opt(g)
	}
	return g
}