package logger

import (
	"context"
	"log/slog"
	"sync/atomic"
)

type ctxKey string

const (
	slogFields     ctxKey     = "slog_fields"
	LevelNotice    slog.Level = slog.Level(2)
	LevelCritical  slog.Level = slog.Level(10)
	LevelAlert     slog.Level = slog.Level(12)
	LevelEmergency slog.Level = slog.Level(14)
)

// Logger wraps slog.Logger with additional methods
type Logger struct {
	logger *slog.Logger
}

// New creates a Logger with the given handler
func New(s *slog.Logger) *Logger {
	return &Logger{logger: s}
}

// Handler returns the Handler associated with the Logger.
func (l *Logger) Handler() slog.Handler {
	return l.logger.Handler()
}

// With returns a Logger that includes the given attributes in each output operation.
func (l *Logger) With(args ...any) *Logger {
	return &Logger{logger: l.logger.With(args...)}
}

// WithGroup returns a Logger that starts a group.
func (l *Logger) WithGroup(name string) *Logger {
	return &Logger{logger: l.logger.WithGroup(name)}
}

// Enabled reports whether the Logger emits log records at the given level.
func (l *Logger) Enabled(ctx context.Context, level slog.Level) bool {
	return l.logger.Enabled(ctx, level)
}

// Log emits a log record with the current time and the given level and message.
func (l *Logger) Log(ctx context.Context, level slog.Level, msg string, args ...any) {
	l.logger.Log(ctx, level, msg, args...)
}

// LogAttrs emits a log record with the current time and the given level and message.
func (l *Logger) LogAttrs(ctx context.Context, level slog.Level, msg string, attrs ...slog.Attr) {
	l.logger.LogAttrs(ctx, level, msg, attrs...)
}

// Debug logs at LevelDebug.
func (l *Logger) Debug(msg string, args ...any) {
	l.logger.Debug(msg, args...)
}

// DebugContext logs at LevelDebug with the given context.
func (l *Logger) DebugContext(ctx context.Context, msg string, args ...any) {
	l.logger.DebugContext(ctx, msg, args...)
}

// Info logs at LevelInfo.
func (l *Logger) Info(msg string, args ...any) {
	l.logger.Info(msg, args...)
}

// InfoContext logs at LevelInfo with the given context.
func (l *Logger) InfoContext(ctx context.Context, msg string, args ...any) {
	l.logger.InfoContext(ctx, msg, args...)
}

// Notice logs at LevelNotice.
func (l *Logger) Notice(msg string, args ...any) {
	l.logger.Log(context.Background(), LevelNotice, msg, args...)
}

// NoticeContext logs at LevelNotice with the given context.
func (l *Logger) NoticeContext(ctx context.Context, msg string, args ...any) {
	l.logger.Log(ctx, LevelNotice, msg, args...)
}

// Warn logs at LevelWarn.
func (l *Logger) Warn(msg string, args ...any) {
	l.logger.Warn(msg, args...)
}

// WarnContext logs at LevelWarn with the given context.
func (l *Logger) WarnContext(ctx context.Context, msg string, args ...any) {
	l.logger.WarnContext(ctx, msg, args...)
}

// Error logs at LevelError.
func (l *Logger) Error(msg string, args ...any) {
	l.logger.Error(msg, args...)
}

// ErrorContext logs at LevelError with the given context.
func (l *Logger) ErrorContext(ctx context.Context, msg string, args ...any) {
	l.logger.ErrorContext(ctx, msg, args...)
}

// Critical logs at LevelCritical.
func (l *Logger) Critical(msg string, args ...any) {
	l.logger.Log(context.Background(), LevelCritical, msg, args...)
}

// CriticalContext logs at LevelCritical with the given context.
func (l *Logger) CriticalContext(ctx context.Context, msg string, args ...any) {
	l.logger.Log(ctx, LevelCritical, msg, args...)
}

// Alert logs at LevelAlert.
func (l *Logger) Alert(msg string, args ...any) {
	l.logger.Log(context.Background(), LevelAlert, msg, args...)
}

// AlertContext logs at LevelAlert with the given context.
func (l *Logger) AlertContext(ctx context.Context, msg string, args ...any) {
	l.logger.Log(ctx, LevelAlert, msg, args...)
}

// Emergency logs at LevelEmergency.
func (l *Logger) Emergency(msg string, args ...any) {
	l.logger.Log(context.Background(), LevelEmergency, msg, args...)
}

// EmergencyContext logs at LevelEmergency with the given context.
func (l *Logger) EmergencyContext(ctx context.Context, msg string, args ...any) {
	l.logger.Log(ctx, LevelEmergency, msg, args...)
}

// Slog returns the underlying *slog.Logger.
func (l *Logger) Slog() *slog.Logger {
	return l.logger
}

// Global default logger
var defaultLogger atomic.Pointer[Logger]

func init() {
	defaultLogger.Store(New(NewPrettySlogger(&PrettyHandlerOptions{
		Level: slog.LevelDebug,
	})))
}

// Default returns the default Logger.
func Default() *Logger {
	return defaultLogger.Load()
}

// SetDefault makes l the default Logger.
func SetDefault(l *Logger) {
	defaultLogger.Store(l)
}

// Top-level convenience functions using the default logger

// Debug logs at LevelDebug using the default logger.
func Debug(msg string, args ...any) {
	Default().Debug(msg, args...)
}

// DebugContext logs at LevelDebug with the given context using the default logger.
func DebugContext(ctx context.Context, msg string, args ...any) {
	Default().DebugContext(ctx, msg, args...)
}

// Info logs at LevelInfo using the default logger.
func Info(msg string, args ...any) {
	Default().Info(msg, args...)
}

// InfoContext logs at LevelInfo with the given context using the default logger.
func InfoContext(ctx context.Context, msg string, args ...any) {
	Default().InfoContext(ctx, msg, args...)
}

// Notice logs at LevelNotice using the default logger.
func Notice(msg string, args ...any) {
	Default().Notice(msg, args...)
}

// NoticeContext logs at LevelNotice with the given context using the default logger.
func NoticeContext(ctx context.Context, msg string, args ...any) {
	Default().NoticeContext(ctx, msg, args...)
}

// Warn logs at LevelWarn using the default logger.
func Warn(msg string, args ...any) {
	Default().Warn(msg, args...)
}

// WarnContext logs at LevelWarn with the given context using the default logger.
func WarnContext(ctx context.Context, msg string, args ...any) {
	Default().WarnContext(ctx, msg, args...)
}

// Error logs at LevelError using the default logger.
func Error(msg string, args ...any) {
	Default().Error(msg, args...)
}

// ErrorContext logs at LevelError with the given context using the default logger.
func ErrorContext(ctx context.Context, msg string, args ...any) {
	Default().ErrorContext(ctx, msg, args...)
}

// Critical logs at LevelCritical using the default logger.
func Critical(msg string, args ...any) {
	Default().Critical(msg, args...)
}

// CriticalContext logs at LevelCritical with the given context using the default logger.
func CriticalContext(ctx context.Context, msg string, args ...any) {
	Default().CriticalContext(ctx, msg, args...)
}

// Alert logs at LevelAlert using the default logger.
func Alert(msg string, args ...any) {
	Default().Alert(msg, args...)
}

// AlertContext logs at LevelAlert with the given context using the default logger.
func AlertContext(ctx context.Context, msg string, args ...any) {
	Default().AlertContext(ctx, msg, args...)
}

// Emergency logs at LevelEmergency using the default logger.
func Emergency(msg string, args ...any) {
	Default().Emergency(msg, args...)
}

// EmergencyContext logs at LevelEmergency with the given context using the default logger.
func EmergencyContext(ctx context.Context, msg string, args ...any) {
	Default().EmergencyContext(ctx, msg, args...)
}

// Log emits a log record with the current time and the given level and message using the default logger.
func Log(ctx context.Context, level slog.Level, msg string, args ...any) {
	Default().Log(ctx, level, msg, args...)
}

// LogAttrs emits a log record with the current time and the given level and message using the default logger.
func LogAttrs(ctx context.Context, level slog.Level, msg string, attrs ...slog.Attr) {
	Default().LogAttrs(ctx, level, msg, attrs...)
}

// Appends an attribute to the context for logging
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
