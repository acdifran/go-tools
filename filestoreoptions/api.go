package filestoreoptions

import "time"

type downloadOptions struct {
	FileName    string
	ContentType string
}

type PresignConfig struct {
	CacheDuration      *time.Duration
	ExpirationDuration time.Duration
	DownloadOptions    *downloadOptions
}

type PresignOption func(*PresignConfig)

func SetCacheDuration(duration time.Duration) PresignOption {
	return func(c *PresignConfig) {
		c.CacheDuration = &duration
	}
}

func SetExpirationDuration(duration time.Duration) PresignOption {
	return func(c *PresignConfig) {
		c.ExpirationDuration = duration
	}
}

func SetDownloadOptions(fileName string, contentType string) PresignOption {
	return func(c *PresignConfig) {
		c.DownloadOptions = &downloadOptions{
			FileName:    fileName,
			ContentType: contentType,
		}
	}
}

type UploadConfig struct {
	CacheDuration *time.Duration
	ContentType   string
	Metadata      map[string]string
}

type UploadOption func(*UploadConfig)

func SetUploadCacheDuration(duration time.Duration) UploadOption {
	return func(c *UploadConfig) {
		c.CacheDuration = &duration
	}
}

func SetUploadContentType(contentType string) UploadOption {
	return func(c *UploadConfig) {
		c.ContentType = contentType
	}
}

func SetUploadMetadata(metadata map[string]string) UploadOption {
	return func(c *UploadConfig) {
		c.Metadata = metadata
	}
}
