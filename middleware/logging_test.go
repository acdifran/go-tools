package middleware

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/acdifran/go-tools/logger"
	"github.com/acdifran/go-tools/redact"
)

// captureLogs points the default logger at a buffer for the duration of a test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := logger.Default()
	logger.SetDefault(logger.New(slog.New(slog.NewJSONHandler(buf, nil))))
	t.Cleanup(func() { logger.SetDefault(prev) })
	return buf
}

func TestRequestLoggingRedactsBodyButPassesOriginalBytesDownstream(t *testing.T) {
	logs := captureLogs(t)

	const body = `{"password":"hunter2","apiKey":"sk-123","keep":"me"}`

	var got string
	handler := AddRequestLogging(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading body: %v", err)
		}
		got = string(b)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	// The handler must see the exact bytes the client sent: webhook signature
	// verification is computed over them.
	if got != body {
		t.Errorf("handler read %q, want %q", got, body)
	}

	out := logs.String()
	for _, secret := range []string{"hunter2", "sk-123"} {
		if strings.Contains(out, secret) {
			t.Errorf("logs leaked %q:\n%s", secret, out)
		}
	}
	if !strings.Contains(out, redact.Redacted) {
		t.Errorf("logs are missing %s:\n%s", redact.Redacted, out)
	}
	if !strings.Contains(out, `keep`) {
		t.Errorf("logs dropped the non-secret fields:\n%s", out)
	}
}

func TestRequestLoggingRedactsErrorResponseBody(t *testing.T) {
	logs := captureLogs(t)

	handler := RequestLogging(redact.New())(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"bad token","refreshToken":"rt-999"}`))
		}),
	)

	req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// The client still gets the untouched response.
	if !strings.Contains(rec.Body.String(), "rt-999") {
		t.Errorf("response to the client was altered: %s", rec.Body.String())
	}
	out := logs.String()
	if strings.Contains(out, "rt-999") {
		t.Errorf("logs leaked the refresh token:\n%s", out)
	}
	if !strings.Contains(out, "bad token") {
		t.Errorf("logs dropped the error message:\n%s", out)
	}
}

func TestRequestLoggingWithExtraKeys(t *testing.T) {
	logs := captureLogs(t)

	handler := RequestLogging(redact.New("ssn"))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
		}),
	)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"ssn":"111-22-3333"}`))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if out := logs.String(); strings.Contains(out, "111-22-3333") {
		t.Errorf("logs leaked the extra key's value:\n%s", out)
	}
}

func TestRequestLoggingLeavesNonJSONBodyAlone(t *testing.T) {
	logs := captureLogs(t)

	const body = "plain text body"
	handler := AddRequestLogging(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
	}))
	handler.ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)),
	)

	if out := logs.String(); !strings.Contains(out, body) {
		t.Errorf("non-JSON body was altered:\n%s", out)
	}
}
