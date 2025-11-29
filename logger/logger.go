package logger

import (
	"context"
	"log/slog"
)

type ctxKey string

const (
	slogFields    ctxKey     = "slog_fields"
	LevelCritical slog.Level = slog.Level(12)
)

func AppendCtx(parent context.Context, attr slog.Attr) context.Context {
	if parent == nil {
		parent = context.Background()
	}

	if v, ok := parent.Value(slogFields).([]slog.Attr); ok {
		v = append(v, attr)
		return context.WithValue(parent, slogFields, v)
	}

	v := []slog.Attr{}
	v = append(v, attr)
	return context.WithValue(parent, slogFields, v)
}
