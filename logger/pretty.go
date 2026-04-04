package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"sync"

	"github.com/acdifran/go-tools/clienterror"
	"github.com/fatih/color"
)

type PrettyHandlerOptions struct {
	Level slog.Leveler
}

type PrettyHandler struct {
	handler slog.Handler
	buf     *bytes.Buffer
	mu      *sync.Mutex
	l       *log.Logger
	attrs   []slog.Attr // Track attributes added via WithAttrs
}

func (h *PrettyHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h *PrettyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &PrettyHandler{
		handler: h.handler.WithAttrs(attrs),
		buf:     h.buf,
		mu:      h.mu,
		l:       h.l,
		attrs:   append(h.attrs, attrs...), // Accumulate attributes
	}
}

func (h *PrettyHandler) WithGroup(name string) slog.Handler {
	return &PrettyHandler{
		handler: h.handler.WithGroup(name),
		buf:     h.buf,
		mu:      h.mu,
		l:       h.l,
		attrs:   h.attrs, // Preserve accumulated attributes
	}
}

func (h *PrettyHandler) Handle(ctx context.Context, r slog.Record) error {
	// Extract context attrs and error attrs separately (don't add to record yet)
	var ctxAttrs []slog.Attr
	if attrs, ok := ctx.Value(slogFields).([]slog.Attr); ok {
		ctxAttrs = attrs
	}

	// Extract error attributes from record attrs
	var errorAttrs []slog.Attr
	r.Attrs(func(attr slog.Attr) bool {
		if err, ok := attr.Value.Any().(error); ok {
			errorAttrs = append(errorAttrs, ExtractAttrs(err)...)

			// Extract client error message if present
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

			// Extract client error message if present
			var cerr *clienterror.Error
			if errors.As(err, &cerr) {
				errorAttrs = append(errorAttrs, slog.String("client_message", cerr.ClientMsg()))
			}
		}
	}

	// Add error attributes to the record
	if len(errorAttrs) > 0 {
		r.AddAttrs(errorAttrs...)
	}

	h.mu.Lock()
	defer h.mu.Unlock()

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

	// Extract and remove built-in fields
	level := fmt.Sprintf("%v:", output["level"])
	msg := fmt.Sprintf("%v", output["msg"])
	delete(output, "level")
	delete(output, "msg")
	delete(output, "time")

	// Color the level based on severity
	switch r.Level {
	case slog.LevelDebug:
		level = color.MagentaString(level)
	case slog.LevelInfo:
		level = color.BlueString(level)
	case LevelNotice:
		level = color.GreenString(level)
	case slog.LevelWarn:
		level = color.YellowString(level)
	case slog.LevelError:
		level = color.RedString(level)
	case LevelCritical:
		level = color.New(color.FgBlack, color.BgRed, color.Bold).Sprintf(" %s ", level)
	case LevelAlert:
		level = color.New(color.FgWhite, color.BgRed, color.Bold).Sprintf(" %s ", level)
	case LevelEmergency:
		level = color.New(color.FgYellow, color.BgRed, color.Bold).Sprintf(" %s ", level)
	}

	// Format remaining fields as indented JSON
	var fieldsStr string
	if len(output) > 0 {
		b, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return err
		}
		fieldsStr = " " + color.WhiteString(string(b))
	}

	timeStr := r.Time.Format("[15:05:05.000]")
	msg = color.CyanString(msg)
	h.l.Println(timeStr, level, msg+fieldsStr)

	return nil
}

func NewPrettyHandler(opts *PrettyHandlerOptions) *PrettyHandler {
	var level slog.Leveler
	if opts != nil && opts.Level != nil {
		level = opts.Level
	} else {
		level = slog.LevelDebug
	}
	hopts := &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if groups != nil {
				return a
			}
			if a.Key == slog.LevelKey {
				switch a.Value.Any().(slog.Level) {
				case LevelNotice:
					a.Value = slog.StringValue("NOTICE")
				case LevelCritical:
					a.Value = slog.StringValue("CRITICAL")
				case LevelAlert:
					a.Value = slog.StringValue("ALERT")
				case LevelEmergency:
					a.Value = slog.StringValue("EMERGENCY")
				case slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError:
					// Keep default severity label for these levels
				}
			}
			return a
		},
	}

	buf := new(bytes.Buffer)

	h := &PrettyHandler{
		handler: slog.NewJSONHandler(buf, hopts),
		buf:     buf,
		mu:      &sync.Mutex{},
		l:       log.New(os.Stdout, "", 0),
		attrs:   []slog.Attr{},
	}

	return h
}

func NewPrettySlogger(opts *PrettyHandlerOptions) *slog.Logger {
	handler := NewPrettyHandler(opts)
	return slog.New(handler)
}
