package s3filestore

import (
	"context"
	"fmt"
	"github.com/acdifran/go-tools/filestoreoptions"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"net/url"
)

func (s *S3FileStore) processOptions(opts ...filestoreoptions.PresignOption) *filestoreoptions.PresignConfig {
	config := &filestoreoptions.PresignConfig{
		CacheDuration:      s.DefaultObjectCacheDuration,
		ExpirationDuration: s.DefaultPresignExpirationDuration,
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

func (s *S3FileStore) PresignedGetUrl(key string, opts ...filestoreoptions.PresignOption) (string, error) {
	getObjectParams := &s3.GetObjectInput{
		Bucket: aws.String(s.BucketName),
		Key:    aws.String(key),
	}

	config := s.processOptions(opts...)
	if config.DownloadOptions != nil {
		fileName := config.DownloadOptions.FileName
		contentType := config.DownloadOptions.ContentType
		asciiFallback := asciiFallbackName(fileName)
		cd := fmt.Sprintf(`attachment; filename=%q; filename*=UTF-8''%s`,
			asciiFallback, url.PathEscape(fileName))

		getObjectParams.ResponseContentDisposition = aws.String(cd)
		getObjectParams.ResponseContentType = aws.String(contentType)
	}

	presigner := s3.NewPresignClient(s.Client)
	resp, err := presigner.PresignGetObject(context.TODO(), getObjectParams,
		s3.WithPresignExpires(config.ExpirationDuration),
	)
	if err != nil {
		return "", fmt.Errorf("signing request, %w", err)
	}

	return resp.URL, nil
}

func (s *S3FileStore) PresignedPutUrl(
	key string,
	opts ...filestoreoptions.PresignOption,
) (string, error) {
	config := s.processOptions(opts...)

	var cacheControl *string
	if config.CacheDuration != nil {
		cacheControl = aws.String(
			fmt.Sprintf("max-age=%d, must-revalidate", int(config.CacheDuration.Seconds())),
		)
	}

	putObjectParams := &s3.PutObjectInput{
		Bucket:       aws.String(s.BucketName),
		Key:          aws.String(key),
		CacheControl: cacheControl,
	}

	presigner := s3.NewPresignClient(s.Client)
	resp, err := presigner.PresignPutObject(context.TODO(), putObjectParams,
		s3.WithPresignExpires(config.ExpirationDuration),
	)
	if err != nil {
		return "", fmt.Errorf("signing request, %w", err)
	}

	return resp.URL, nil
}

func (s *S3FileStore) DeleteObject(ctx context.Context, key string) error {
	_, err := s.Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.BucketName),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("deleting object %q: %w", key, err)
	}

	return nil
}
