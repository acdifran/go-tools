package gcloudfilestore

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/acdifran/go-tools/filestoreoptions"

	"cloud.google.com/go/storage"
)

func (g *GCloudFileStore) processOptions(opts ...filestoreoptions.PresignOption) *filestoreoptions.PresignConfig {
	config := &filestoreoptions.PresignConfig{
		CacheDuration:      g.DefaultObjectCacheDuration,
		ExpirationDuration: g.DefaultPresignExpirationDuration,
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

func (g *GCloudFileStore) PresignedGetUrl(key string, opts ...filestoreoptions.PresignOption) (string, error) {
	config := g.processOptions(opts...)

	signOpts := &storage.SignedURLOptions{
		Method:  "GET",
		Expires: time.Now().Add(config.ExpirationDuration),
	}

	if config.DownloadOptions != nil {
		fileName := config.DownloadOptions.FileName
		contentType := config.DownloadOptions.ContentType
		asciiFallback := asciiFallbackName(fileName)
		cd := fmt.Sprintf(`attachment; filename=%q; filename*=UTF-8''%s`,
			asciiFallback, url.PathEscape(fileName))

		signOpts.QueryParameters = url.Values{
			"response-content-disposition": {cd},
			"response-content-type":        {contentType},
		}
	}

	if g.ServiceAccount != "" {
		signOpts.GoogleAccessID = g.ServiceAccount
	}

	signedURL, err := g.Client.Bucket(g.BucketName).SignedURL(key, signOpts)
	if err != nil {
		return "", fmt.Errorf("signing request, %w", err)
	}

	return signedURL, nil
}

func (g *GCloudFileStore) PresignedPutUrl(
	key string,
	opts ...filestoreoptions.PresignOption,
) (string, error) {
	config := g.processOptions(opts...)

	signOpts := &storage.SignedURLOptions{
		Method:      "PUT",
		ContentType: "application/octet-stream",
		Expires:     time.Now().Add(config.ExpirationDuration),
	}

	if config.CacheDuration != nil {
		signOpts.Headers = []string{
			fmt.Sprintf("Cache-Control:max-age=%d, must-revalidate", int(config.CacheDuration.Seconds())),
		}
	}

	if g.ServiceAccount != "" {
		signOpts.GoogleAccessID = g.ServiceAccount
	}

	signedURL, err := g.Client.Bucket(g.BucketName).SignedURL(key, signOpts)
	if err != nil {
		return "", fmt.Errorf("signing request, %w", err)
	}

	return signedURL, nil
}

func (g *GCloudFileStore) DeleteObject(ctx context.Context, key string) error {
	err := g.Client.Bucket(g.BucketName).Object(key).Delete(ctx)
	if err != nil {
		return fmt.Errorf("deleting object %q: %w", key, err)
	}

	return nil
}

func (g *GCloudFileStore) DownloadFile(ctx context.Context, key string) ([]byte, error) {
	rc, err := g.Client.Bucket(g.BucketName).Object(key).NewReader(ctx)
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", key, err)
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", key, err)
	}

	return data, nil
}

func (g *GCloudFileStore) GetPath(key string) string {
	return fmt.Sprintf("gs://%s/%s", g.BucketName, key)
}

func (g *GCloudFileStore) processUploadOptions(opts ...filestoreoptions.UploadOption) *filestoreoptions.UploadConfig {
	config := &filestoreoptions.UploadConfig{
		CacheDuration: g.DefaultObjectCacheDuration,
	}

	for _, opt := range opts {
		opt(config)
	}

	return config
}

func (g *GCloudFileStore) UploadFile(
	ctx context.Context,
	key string,
	data io.Reader,
	opts ...filestoreoptions.UploadOption,
) error {
	config := g.processUploadOptions(opts...)

	writer := g.Client.Bucket(g.BucketName).Object(key).NewWriter(ctx)
	if config.CacheDuration != nil {
		writer.CacheControl = fmt.Sprintf("max-age=%d, must-revalidate", int(config.CacheDuration.Seconds()))
	}
	if config.ContentType != "" {
		writer.ContentType = config.ContentType
	}
	if config.Metadata != nil {
		writer.Metadata = config.Metadata
	}

	if _, err := io.Copy(writer, data); err != nil {
		writer.Close()
		return fmt.Errorf("uploading %s: %w", key, err)
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("uploading %s: %w", key, err)
	}

	return nil
}
