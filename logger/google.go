package logger

import (
	"bytes"
	"context"
	"encoding/json"
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
	buf     *bytes.Buffer
	attrs   []slog.Attr // Track attributes added via WithAttrs
}

func (h *GoogleHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h *GoogleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &GoogleHandler{
		handler: h.handler.WithAttrs(attrs),
		buf:     h.buf,
		attrs:   append(h.attrs, attrs...), // Accumulate attributes
	}
}

func (h *GoogleHandler) WithGroup(name string) slog.Handler {
	return &GoogleHandler{
		handler: h.handler.WithGroup(name),
		buf:     h.buf,
		attrs:   h.attrs, // Preserve accumulated attributes
	}
}

func (h *GoogleHandler) Handle(ctx context.Context, r slog.Record) error {
	// Extract context attrs separately (don't add to record yet)
	var ctxAttrs []slog.Attr
	if attrs, ok := ctx.Value(slogFields).([]slog.Attr); ok {
		ctxAttrs = attrs
	}

	// Extract error attributes from any attribute value that's an error
	var errorAttrs []slog.Attr
	r.Attrs(func(attr slog.Attr) bool {
		if err, ok := attr.Value.Any().(error); ok {
			errorAttrs = append(errorAttrs, ExtractAttrs(err)...)
			var cerr *clienterror.Error
			if errors.As(err, &cerr) {
				errorAttrs = append(errorAttrs, slog.String("client_message", cerr.ClientMsg()))
			}
		}
		return true
	})

	// Also check handler's accumulated attributes (from WithAttrs/WithError)
	for _, attr := range h.attrs {
		if err, ok := attr.Value.Any().(error); ok {
			errorAttrs = append(errorAttrs, ExtractAttrs(err)...)
			var cerr *clienterror.Error
			if errors.As(err, &cerr) {
				errorAttrs = append(errorAttrs, slog.String("client_message", cerr.ClientMsg()))
			}
		}
	}

	// Add only error attributes to the record (they should be grouped)
	if len(errorAttrs) > 0 {
		r.AddAttrs(errorAttrs...)
	}

	h.buf.Reset()
	if err := h.handler.Handle(ctx, r); err != nil {
		return err
	}

	// Parse the JSON output
	var output map[string]any
	if err := json.Unmarshal(h.buf.Bytes(), &output); err != nil {
		return err
	}

	// Add context attrs directly to output (bypasses groups)
	for _, attr := range ctxAttrs {
		output[attr.Key] = attr.Value.Any()
	}

	// Re-encode and write to stdout
	encoder := json.NewEncoder(os.Stdout)
	return encoder.Encode(output)
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

	buf := new(bytes.Buffer)

	return &GoogleHandler{
		handler: slog.NewJSONHandler(buf, hopts),
		buf:     buf,
		attrs:   []slog.Attr{}, // Initialize empty attrs slice
	}
}

func NewGoogleSlogger(opts *GoogleHandlerOptions) *slog.Logger {
	handler := NewGoogleHandler(opts)
	return slog.New(handler)
}
