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
