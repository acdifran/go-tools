package gcloudfilestore

import (
	"fmt"
	"github.com/acdifran/go-tools/filestoreoptions"
	"net/url"
	"time"

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
		Method:  "PUT",
		Expires: time.Now().Add(config.ExpirationDuration),
	}

	if config.CacheDuration != nil {
		signOpts.Headers = []string{
			fmt.Sprintf("Cache-Control:max-age=%d, must-revalidate", int(config.CacheDuration.Seconds())),
		}
	}

	signedURL, err := g.Client.Bucket(g.BucketName).SignedURL(key, signOpts)
	if err != nil {
		return "", fmt.Errorf("signing request, %w", err)
	}

	return signedURL, nil
}
