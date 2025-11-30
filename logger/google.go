package logger

import (
	"context"
	"errors"
	"log/slog"
	"os"

	"github.com/acdifran/go-tools/clienterror"
)

type GoogleHandlerOptions struct {
	Level slog.Leveler
}

type GoogleHandler struct {
	handler slog.Handler
}

func (h *GoogleHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h *GoogleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &GoogleHandler{
		handler: h.handler.WithAttrs(attrs),
	}
}

func (h *GoogleHandler) WithGroup(name string) slog.Handler {
	return &GoogleHandler{
		handler: h.handler.WithGroup(name),
	}
}

func (h *GoogleHandler) Handle(ctx context.Context, r slog.Record) error {
	// Add context attrs to the record
	if attrs, ok := ctx.Value(slogFields).([]slog.Attr); ok {
		for _, attr := range attrs {
			r.AddAttrs(attr)
		}
	}

	// Extract error attributes from any attribute value that's an error
	var errorAttrs []slog.Attr
	r.Attrs(func(attr slog.Attr) bool {
		if err, ok := attr.Value.Any().(error); ok {
			// Extract attributes from wrapped error
			errorAttrs = append(errorAttrs, ExtractAttrs(err)...)

			// Extract client error message if present
			var cerr *clienterror.Error
			if errors.As(err, &cerr) {
				errorAttrs = append(errorAttrs, slog.String("client_message", cerr.ClientMsg()))
			}
		}
		return true
	})

	// Add collected error attributes to the record
	if len(errorAttrs) > 0 {
		r.AddAttrs(errorAttrs...)
	}

	return h.handler.Handle(ctx, r)
}

func NewGoogleHandler(opts *GoogleHandlerOptions) *GoogleHandler {
	var level slog.Leveler
	if opts != nil && opts.Level != nil {
		level = opts.Level
	} else {
		level = slog.LevelDebug
	}
	hopts := &slog.HandlerOptions{
		AddSource: true,
		Level:     level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if groups != nil {
				return a
			}
			switch a.Key {
			case slog.MessageKey:
				a.Key = "message"
			case slog.SourceKey:
				a.Key = "logging.googleapis.com/sourceLocation"
			case slog.LevelKey:
				a.Key = "severity"
				level := a.Value.Any().(slog.Level)
				switch level {
				case LevelNotice:
					a.Value = slog.StringValue("NOTICE")
				case slog.LevelWarn:
					a.Value = slog.StringValue("WARNING")
				case LevelCritical:
					a.Value = slog.StringValue("CRITICAL")
				case LevelAlert:
					a.Value = slog.StringValue("ALERT")
				case LevelEmergency:
					a.Value = slog.StringValue("EMERGENCY")
				case slog.LevelDebug, slog.LevelInfo, slog.LevelError:
					// Keep default severity label for these levels
				}
			}
			return a
		},
	}

	return &GoogleHandler{
		handler: slog.NewJSONHandler(os.Stdout, hopts),
	}
}

func NewGoogleSlogger(opts *GoogleHandlerOptions) *slog.Logger {
	handler := NewGoogleHandler(opts)
	return slog.New(handler)
}
