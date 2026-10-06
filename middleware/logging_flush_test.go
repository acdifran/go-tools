package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCustomResponseWriterFlushes(t *testing.T) {
	rec := httptest.NewRecorder()
	crw := newCustomResponseWriter(rec)
	if _, err := crw.Write([]byte("data: hi\n\n")); err != nil {
		t.Fatal(err)
	}
	if err := http.NewResponseController(crw).Flush(); err != nil {
		t.Fatalf("flush through the logging writer: %v", err)
	}
	if !rec.Flushed {
		t.Fatal("the underlying writer was not flushed")
	}
}
