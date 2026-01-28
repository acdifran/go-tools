package gcloudfilestore

import (
	"fmt"
	"net/url"
	"time"

	"cloud.google.com/go/storage"
)

type downloadOptions struct {
	fileName    string
	contentType string
}

type PresignConfig struct {
	cacheDuration      *time.Duration
	expirationDuration time.Duration
	downloadOptions    *downloadOptions
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

func SetDownloadOptions(fileName string, contentType string) PresignOption {
	return func(c *PresignConfig) {
		c.downloadOptions = &downloadOptions{
			fileName:    fileName,
			contentType: contentType,
		}
	}
}

func (g *GCloudFileStore) processOptions(opts ...PresignOption) *PresignConfig {
	config := &PresignConfig{
		cacheDuration:      g.DefaultObjectCacheDuration,
		expirationDuration: g.DefaultPresignExpirationDuration,
	}

	for _, opt := range opts {
		opt(config)
	}

	return config
}

func asciiFallbackName(s string) string {
	out := make([]rune, len(s))
	for i, r := range s {
		if r < 128 {
			out[i] = r
		} else {
			out[i] = '_'
		}
	}
	return string(out)
}

func (g *GCloudFileStore) PresignedGetUrl(key string, opts ...PresignOption) (string, error) {
	config := g.processOptions(opts...)

	signOpts := &storage.SignedURLOptions{
		Method:  "GET",
		Expires: time.Now().Add(config.expirationDuration),
	}

	if config.downloadOptions != nil {
		fileName := config.downloadOptions.fileName
		contentType := config.downloadOptions.contentType
		asciiFallback := asciiFallbackName(fileName)
		cd := fmt.Sprintf(`attachment; filename=%q; filename*=UTF-8''%s`,
			asciiFallback, url.PathEscape(fileName))

		signOpts.QueryParameters = url.Values{
			"response-content-disposition": {cd},
			"response-content-type":        {contentType},
		}
	}

	signedURL, err := g.Client.Bucket(g.BucketName).SignedURL(key, signOpts)
	if err != nil {
		return "", fmt.Errorf("signing request, %w", err)
	}

	return signedURL, nil
}

func (g *GCloudFileStore) PresignedPutUrl(
	key string,
	opts ...PresignOption,
) (string, error) {
	config := g.processOptions(opts...)

	signOpts := &storage.SignedURLOptions{
		Method:  "PUT",
		Expires: time.Now().Add(config.expirationDuration),
	}

	if config.cacheDuration != nil {
		signOpts.Headers = []string{
			fmt.Sprintf("Cache-Control:max-age=%d, must-revalidate", int(config.cacheDuration.Seconds())),
		}
	}

	signedURL, err := g.Client.Bucket(g.BucketName).SignedURL(key, signOpts)
	if err != nil {
		return "", fmt.Errorf("signing request, %w", err)
	}

	return signedURL, nil
}