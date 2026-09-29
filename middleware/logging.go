package middleware

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/acdifran/go-tools/logger"
	"github.com/acdifran/go-tools/redact"
	"github.com/google/uuid"
)

type customResponseWriter struct {
	http.ResponseWriter
	statusCode int
	body       *bytes.Buffer
}

func newCustomResponseWriter(w http.ResponseWriter) *customResponseWriter {
	return &customResponseWriter{w, http.StatusOK, bytes.NewBuffer(nil)}
}

func (crw *customResponseWriter) WriteHeader(statusCode int) {
	crw.statusCode = statusCode
	crw.ResponseWriter.WriteHeader(statusCode)
}

func (crw *customResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := crw.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("hijack not supported")
	}
	return h.Hijack()
}

func (crw *customResponseWriter) Write(b []byte) (int, error) {
	crw.body.Write(b) // Capture the body
	return crw.ResponseWriter.Write(b)
}

// defaultRedactor is compiled once: AddRequestLogging may be called per request
// when it is wrapped in UseIf or SkipIf.
var defaultRedactor = redact.New()

// RequestLogging logs every request and any failed response. Secret values are
// hidden by r before they are logged; the handler downstream still reads the
// exact bytes the client sent, which webhook signature verification depends on.
func RequestLogging(r *redact.Redactor) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := req.Context()
			startTime := time.Now()
			ctx = logger.AppendCtx(ctx, slog.String("request_id", uuid.NewString()))

			body, err := io.ReadAll(req.Body)
			if err != nil {
				logger.ErrorContext(ctx, "Error reading body", "error", err)
				http.Error(w, "can't read body", http.StatusBadRequest)
				return
			}
			req.Body = io.NopCloser(bytes.NewReader(body))

			requestLogger := logger.Default().WithGroup("request").With(
				slog.String("method", req.Method),
				slog.String("path", req.URL.Path),
				slog.String("remote_addr", req.RemoteAddr),
				slog.String("user_agent", req.UserAgent()),
				slog.String("referer", req.Referer()),
				slog.String("body", string(r.JSON(body))),
			)

			requestLogger.InfoContext(ctx, "Request Started")

			crw := newCustomResponseWriter(w)
			next.ServeHTTP(crw, req.WithContext(ctx))

			if crw.statusCode >= 400 {
				requestLogger.ErrorContext(
					ctx,
					"Request Failed",
					slog.Int("response_code", crw.statusCode),
					slog.String("response_body", string(r.JSON(crw.body.Bytes()))),
				)
			}

			requestLogger.InfoContext(
				ctx,
				"Request Finished",
				slog.Int64("duration_ms", time.Since(startTime).Milliseconds()),
				slog.Int("response_code", crw.statusCode),
			)
		})
	}
}

// AddRequestLogging is RequestLogging with the default redaction keys.
func AddRequestLogging(next http.Handler) http.Handler {
	return RequestLogging(defaultRedactor)(next)
}

func AddViewerToLogs(fromContext func(context.Context) any) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			vc := fromContext(ctx)
			ctx = logger.AppendCtx(ctx, slog.Any("viewer", vc))
			next.ServeHTTP(
				w,
				r.WithContext(ctx),
			)
		})
	}
}
