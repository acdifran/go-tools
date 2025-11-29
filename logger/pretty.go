package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"os"

	"github.com/fatih/color"
)

type PrettyHandlerOptions struct {
	Level slog.Leveler
}

type PrettyHandler struct {
	handler slog.Handler
	buf     *bytes.Buffer
	l       *log.Logger
}

func (h *PrettyHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h *PrettyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &PrettyHandler{
		handler: h.handler.WithAttrs(attrs),
		buf:     h.buf,
		l:       h.l,
	}
}

func (h *PrettyHandler) WithGroup(name string) slog.Handler {
	return &PrettyHandler{
		handler: h.handler.WithGroup(name),
		buf:     h.buf,
		l:       h.l,
	}
}

func (h *PrettyHandler) Handle(ctx context.Context, r slog.Record) error {
	// Add context attrs to the record
	if attrs, ok := ctx.Value(slogFields).([]slog.Attr); ok {
		for _, attr := range attrs {
			r.AddAttrs(attr)
		}
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
	case slog.LevelWarn:
		level = color.YellowString(level)
	case slog.LevelError:
		level = color.RedString(level)
	case LevelCritical:
		level = color.HiRedString(level)
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
	}

	buf := new(bytes.Buffer)

	h := &PrettyHandler{
		handler: slog.NewJSONHandler(buf, hopts),
		buf:     buf,
		l:       log.New(os.Stdout, "", 0),
	}

	return h
}

func NewPrettyLogger(opts *PrettyHandlerOptions) *slog.Logger {
	handler := NewPrettyHandler(opts)
	return slog.New(handler)
}
