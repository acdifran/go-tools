package logger

import (
	"errors"
	"log/slog"
)

type ErrorWithAttrs struct {
	err   error
	attrs []slog.Attr
}

func (e *ErrorWithAttrs) Error() string {
	return e.err.Error()
}

func (e *ErrorWithAttrs) Unwrap() error {
	return e.err
}

// WrapError wraps an error with log attributes
func WrapError(err error, attrs ...slog.Attr) error {
	if err == nil {
		return nil
	}

	// If already wrapped, append to existing attrs
	var existing *ErrorWithAttrs
	if errors.As(err, &existing) {
		return &ErrorWithAttrs{
			err:   existing.err,
			attrs: append(existing.attrs, attrs...),
		}
	}

	return &ErrorWithAttrs{
		err:   err,
		attrs: attrs,
	}
}

// ExtractAttrs extracts all log attributes from an error chain
func ExtractAttrs(err error) []slog.Attr {
	var attrs []slog.Attr
	var e *ErrorWithAttrs

	// Walk the error chain collecting all attributes
	for err != nil {
		if errors.As(err, &e) {
			attrs = append(attrs, e.attrs...)
		}
		err = errors.Unwrap(err)
	}

	return attrs
}
