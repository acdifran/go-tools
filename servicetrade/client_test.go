package servicetrade

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func makeJWT(exp time.Time) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, exp.Unix())))
	return header + "." + payload + ".signature"
}

func validToken() string { return makeJWT(time.Now().Add(time.Hour)) }

func expiredToken() string { return makeJWT(time.Now().Add(-time.Minute)) }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func newServer(t *testing.T) (*httptest.Server, *http.ServeMux) {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, mux
}

func newTestClient(t *testing.T, srv *httptest.Server, opts ...Option) *Client {
	t.Helper()
	c, err := NewClient(append([]Option{WithBaseURL(srv.URL)}, opts...)...)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	c.retryBackoff = 0
	return c
}

// tokenEndpoint serves /oauth2/token, handing out tokens in order (the last
// one repeats) and recording each request.
type tokenEndpoint struct {
	mu      sync.Mutex
	tokens  []string
	forms   []url.Values
	headers []http.Header
}

func newTokenEndpoint(tokens ...string) *tokenEndpoint {
	return &tokenEndpoint{mu: sync.Mutex{}, tokens: tokens, forms: nil, headers: nil}
}

func (te *tokenEndpoint) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parsing token form: %v", err)
		}
		te.mu.Lock()
		te.forms = append(te.forms, r.PostForm)
		te.headers = append(te.headers, r.Header.Clone())
		token := te.tokens[min(len(te.forms), len(te.tokens))-1]
		te.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"access_token": token})
	}
}

func (te *tokenEndpoint) calls() int {
	te.mu.Lock()
	defer te.mu.Unlock()
	return len(te.forms)
}

func (te *tokenEndpoint) form(i int) url.Values {
	te.mu.Lock()
	defer te.mu.Unlock()
	return te.forms[i]
}

func (te *tokenEndpoint) header(i int) http.Header {
	te.mu.Lock()
	defer te.mu.Unlock()
	return te.headers[i]
}

type recordedRequest struct {
	Method string
	URL    *url.URL
	Header http.Header
	Body   []byte
}

// recorder captures API requests received by a test server.
type recorder struct {
	mu   sync.Mutex
	reqs []recordedRequest
}

func (rec *recorder) record(r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	rec.mu.Lock()
	defer rec.mu.Unlock()
	rec.reqs = append(rec.reqs, recordedRequest{
		Method: r.Method,
		URL:    r.URL,
		Header: r.Header.Clone(),
		Body:   body,
	})
}

func (rec *recorder) count() int {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return len(rec.reqs)
}

func (rec *recorder) get(i int) recordedRequest {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.reqs[i]
}

func dataResponse(v any) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"data": v})
	}
}

func TestNewClient_RequiresCredentials(t *testing.T) {
	if _, err := NewClient(); !errors.Is(err, ErrMissingCredentials) {
		t.Fatalf("NewClient() error = %v, want ErrMissingCredentials", err)
	}
	if _, err := NewClient(WithClientCredentials("id", "")); !errors.Is(err, ErrMissingCredentials) {
		t.Fatalf("NewClient(id only) error = %v, want ErrMissingCredentials", err)
	}
	for name, opt := range map[string]Option{
		"client credentials": WithClientCredentials("id", "secret"),
		"refresh token":      WithRefreshToken("rt"),
		"token":              WithToken("tok"),
	} {
		if _, err := NewClient(opt); err != nil {
			t.Errorf("NewClient(%s) error = %v", name, err)
		}
	}
}

func TestLogin_ClientCredentials(t *testing.T) {
	srv, mux := newServer(t)
	token := validToken()
	te := newTokenEndpoint(token)
	mux.HandleFunc("POST /api/oauth2/token", te.handler(t))

	var callbackToken string
	c := newTestClient(t, srv,
		WithClientCredentials("id", "secret"),
		WithUserAgent("Test Agent/1.0"),
		WithOnSetAuth(func(tok string) { callbackToken = tok }),
	)

	got, err := c.Login(context.Background())
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if got != token {
		t.Errorf("Login returned %q, want %q", got, token)
	}
	if c.Token() != token {
		t.Errorf("Token() = %q, want %q", c.Token(), token)
	}
	if callbackToken != token {
		t.Errorf("OnSetAuth received %q, want %q", callbackToken, token)
	}

	form := te.form(0)
	for key, want := range map[string]string{
		"grant_type":    "client_credentials",
		"client_id":     "id",
		"client_secret": "secret",
	} {
		if got := form.Get(key); got != want {
			t.Errorf("form[%s] = %q, want %q", key, got, want)
		}
	}
	hdr := te.header(0)
	if got := hdr.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := hdr.Get("User-Agent"); got != "Test Agent/1.0" {
		t.Errorf("User-Agent = %q", got)
	}
}

func TestLogin_RefreshTokenTakesPriorityAndRotates(t *testing.T) {
	srv, mux := newServer(t)
	var (
		mu    sync.Mutex
		forms []url.Values
	)
	mux.HandleFunc("POST /api/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		mu.Lock()
		forms = append(forms, r.PostForm)
		n := len(forms)
		mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{
			"access_token":  validToken(),
			"refresh_token": fmt.Sprintf("rt-%d", n),
		})
	})

	c := newTestClient(t, srv, WithClientCredentials("id", "secret"), WithRefreshToken("rt-0"))
	for range 2 {
		if _, err := c.Login(context.Background()); err != nil {
			t.Fatalf("Login: %v", err)
		}
	}
	if got := c.RefreshToken(); got != "rt-2" {
		t.Errorf("RefreshToken() = %q, want rt-2", got)
	}

	mu.Lock()
	defer mu.Unlock()
	if got := forms[0].Get("grant_type"); got != "refresh_token" {
		t.Errorf("grant_type = %q, want refresh_token", got)
	}
	if got := forms[0].Get("client_id"); got != "" {
		t.Errorf("client_id should not be sent with the refresh grant, got %q", got)
	}
	if got := forms[0].Get("refresh_token"); got != "rt-0" {
		t.Errorf("first refresh_token = %q, want rt-0", got)
	}
	if got := forms[1].Get("refresh_token"); got != "rt-1" {
		t.Errorf("second refresh_token = %q, want rotated rt-1", got)
	}
}

func TestLogin_Failures(t *testing.T) {
	srv, mux := newServer(t)
	mux.HandleFunc("POST /api/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostForm.Get("client_id") == "bad" {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid_client"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{})
	})

	c := newTestClient(t, srv, WithClientCredentials("bad", "secret"))
	_, err := c.Get(context.Background(), "/job/1", nil, nil)
	var authErr *AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("Get error = %v, want *AuthError", err)
	}
	if authErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want 401", authErr.StatusCode)
	}
	if !strings.Contains(string(authErr.Body), "invalid_client") {
		t.Errorf("Body = %q, want invalid_client", authErr.Body)
	}

	c = newTestClient(t, srv, WithClientCredentials("id", "secret"))
	_, err = c.Login(context.Background())
	if !errors.As(err, &authErr) {
		t.Fatalf("Login error = %v, want *AuthError", err)
	}
	if authErr.StatusCode != http.StatusOK || authErr.Err == nil {
		t.Errorf("missing access_token should produce status 200 with an inner error, got %+v", authErr)
	}

	c = newTestClient(t, srv, WithToken(validToken()))
	if _, err := c.Login(context.Background()); !errors.Is(err, ErrMissingCredentials) {
		t.Errorf("token-only Login error = %v, want ErrMissingCredentials", err)
	}
}

func TestGet_LazyLoginUnwrapsData(t *testing.T) {
	srv, mux := newServer(t)
	token := validToken()
	te := newTokenEndpoint(token)
	rec := &recorder{}
	mux.HandleFunc("POST /api/oauth2/token", te.handler(t))
	mux.HandleFunc("GET /api/job/1", func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"id": 1, "name": "Job"}})
	})

	c := newTestClient(t, srv, WithClientCredentials("id", "secret"))
	var job struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	resp, err := c.Get(context.Background(), "/job/1", nil, &job)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d", resp.StatusCode)
	}
	if job.ID != 1 || job.Name != "Job" {
		t.Errorf("decoded job = %+v", job)
	}
	if te.calls() != 1 {
		t.Errorf("token endpoint called %d times, want 1", te.calls())
	}
	if got := rec.get(0).Header.Get("Authorization"); got != "Bearer "+token {
		t.Errorf("Authorization = %q", got)
	}
	if !strings.Contains(string(resp.Body), `"data"`) {
		t.Errorf("Response.Body should hold the full payload, got %s", resp.Body)
	}
}

func TestGet_ListPlainAndEmptyResponses(t *testing.T) {
	srv, mux := newServer(t)
	mux.HandleFunc("GET /api/list", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, []map[string]any{{"id": 1}, {"id": 2}})
	})
	mux.HandleFunc("GET /api/plain", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"id": 7})
	})
	mux.HandleFunc("GET /api/empty", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	c := newTestClient(t, srv, WithToken(validToken()))
	ctx := context.Background()

	var items []struct {
		ID int `json:"id"`
	}
	if _, err := c.Get(ctx, "/list", nil, &items); err != nil {
		t.Fatalf("Get list: %v", err)
	}
	if len(items) != 2 || items[1].ID != 2 {
		t.Errorf("list decoded as %+v", items)
	}

	var plain struct {
		ID int `json:"id"`
	}
	if _, err := c.Get(ctx, "/plain", nil, &plain); err != nil {
		t.Fatalf("Get plain: %v", err)
	}
	if plain.ID != 7 {
		t.Errorf("plain decoded as %+v", plain)
	}

	out := map[string]any{"untouched": true}
	resp, err := c.Get(ctx, "/empty", nil, &out)
	if err != nil {
		t.Fatalf("Get empty: %v", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("StatusCode = %d", resp.StatusCode)
	}
	if _, ok := out["untouched"]; !ok {
		t.Errorf("empty body should leave out untouched, got %v", out)
	}
}

func TestPostAndPut_SendJSONBodyAndParams(t *testing.T) {
	srv, mux := newServer(t)
	rec := &recorder{}
	handler := func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		writeJSON(w, http.StatusCreated, map[string]any{"data": map[string]any{"id": 5}})
	}
	mux.HandleFunc("POST /api/job", handler)
	mux.HandleFunc("PUT /api/job/5", handler)
	c := newTestClient(t, srv, WithToken(validToken()))
	ctx := context.Background()

	body := map[string]any{"type": "inspection"}
	params := url.Values{"force": {"true"}}
	var out struct {
		ID int `json:"id"`
	}
	resp, err := c.Post(ctx, "/job", params, body, &out)
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("Post StatusCode = %d", resp.StatusCode)
	}
	if _, err := c.Put(ctx, "/job/5", params, body, &out); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if out.ID != 5 {
		t.Errorf("decoded id = %d, want 5", out.ID)
	}

	for i, method := range []string{http.MethodPost, http.MethodPut} {
		req := rec.get(i)
		if req.Method != method {
			t.Errorf("request %d method = %s, want %s", i, req.Method, method)
		}
		if got := req.URL.Query().Get("force"); got != "true" {
			t.Errorf("request %d force = %q", i, got)
		}
		if got := req.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("request %d Content-Type = %q", i, got)
		}
		if got := strings.TrimSpace(string(req.Body)); got != `{"type":"inspection"}` {
			t.Errorf("request %d body = %s", i, got)
		}
	}
}

func TestDelete(t *testing.T) {
	srv, mux := newServer(t)
	rec := &recorder{}
	mux.HandleFunc("DELETE /api/job/123", func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"deleted": true}})
	})
	c := newTestClient(t, srv, WithToken(validToken()))

	resp, err := c.Delete(context.Background(), "/job/123", url.Values{"force": {"true"}})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d", resp.StatusCode)
	}
	req := rec.get(0)
	if req.Method != http.MethodDelete {
		t.Errorf("method = %s", req.Method)
	}
	if got := req.URL.Query().Get("force"); got != "true" {
		t.Errorf("force = %q", got)
	}
	if len(req.Body) != 0 {
		t.Errorf("DELETE should not send a body, got %s", req.Body)
	}
	if got := req.Header.Get("Content-Type"); got != "" {
		t.Errorf("DELETE without body should not send Content-Type, got %q", got)
	}
}

func TestTokenOnly_UsesTokenWithoutLogin(t *testing.T) {
	srv, mux := newServer(t)
	rec := &recorder{}
	mux.HandleFunc("POST /api/oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		t.Error("token endpoint should not be called for a token-only client")
		w.WriteHeader(http.StatusInternalServerError)
	})
	mux.HandleFunc("GET /api/job/1", func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"id": 1}})
	})
	token := validToken()
	c := newTestClient(t, srv, WithToken(token))

	var job struct {
		ID int `json:"id"`
	}
	if _, err := c.Get(context.Background(), "/job/1", nil, &job); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if job.ID != 1 {
		t.Errorf("job = %+v", job)
	}
	if got := rec.get(0).Header.Get("Authorization"); got != "Bearer "+token {
		t.Errorf("Authorization = %q", got)
	}
}

func TestTokenOnly_401IsNotRetried(t *testing.T) {
	srv, mux := newServer(t)
	var calls atomic.Int32
	mux.HandleFunc("GET /api/job/1", func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writeJSON(w, http.StatusUnauthorized, map[string]any{"messages": map[string]any{"error": "expired"}})
	})
	c := newTestClient(t, srv, WithToken(validToken()))

	_, err := c.Get(context.Background(), "/job/1", nil, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("Get error = %v, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d", apiErr.StatusCode)
	}
	if calls.Load() != 1 {
		t.Errorf("endpoint called %d times, want 1", calls.Load())
	}
}

func TestRefreshOn401(t *testing.T) {
	srv, mux := newServer(t)
	oldToken := makeJWT(time.Now().Add(time.Hour))
	newToken := makeJWT(time.Now().Add(2 * time.Hour))
	te := newTokenEndpoint(oldToken, newToken)
	rec := &recorder{}
	mux.HandleFunc("POST /api/oauth2/token", te.handler(t))
	mux.HandleFunc("GET /api/job/123", func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		if rec.count() == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"id": 123}})
	})
	c := newTestClient(t, srv, WithClientCredentials("id", "secret"))

	if _, err := c.Login(context.Background()); err != nil {
		t.Fatalf("Login: %v", err)
	}
	var job struct {
		ID int `json:"id"`
	}
	if _, err := c.Get(context.Background(), "/job/123", nil, &job); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if job.ID != 123 {
		t.Errorf("job = %+v", job)
	}
	if rec.count() != 2 {
		t.Errorf("endpoint called %d times, want 2", rec.count())
	}
	if te.calls() != 2 {
		t.Errorf("token endpoint called %d times, want 2", te.calls())
	}
	if got := rec.get(1).Header.Get("Authorization"); got != "Bearer "+newToken {
		t.Errorf("retry Authorization = %q, want the refreshed token", got)
	}
	if c.Token() != newToken {
		t.Errorf("Token() = %q, want refreshed token", c.Token())
	}
}

func TestRefreshStaleTokenBeforeRequest(t *testing.T) {
	srv, mux := newServer(t)
	newToken := validToken()
	te := newTokenEndpoint(newToken)
	rec := &recorder{}
	mux.HandleFunc("POST /api/oauth2/token", te.handler(t))
	mux.HandleFunc("GET /api/job/123", func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"id": 123}})
	})
	c := newTestClient(t, srv, WithToken(expiredToken()), WithClientCredentials("id", "secret"))

	if _, err := c.Get(context.Background(), "/job/123", nil, nil); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if te.calls() != 1 {
		t.Errorf("token endpoint called %d times, want 1", te.calls())
	}
	if got := rec.get(0).Header.Get("Authorization"); got != "Bearer "+newToken {
		t.Errorf("Authorization = %q, want the refreshed token", got)
	}
}

func TestStaleToken_NoAutoRefresh(t *testing.T) {
	srv, mux := newServer(t)
	rec := &recorder{}
	mux.HandleFunc("POST /api/oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		t.Error("token endpoint should not be called when auto-refresh is off")
		w.WriteHeader(http.StatusInternalServerError)
	})
	mux.HandleFunc("GET /api/job/1", func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"id": 1}})
	})
	stale := expiredToken()
	c := newTestClient(t, srv,
		WithToken(stale),
		WithClientCredentials("id", "secret"),
		WithAutoRefreshAuth(false),
	)

	if _, err := c.Get(context.Background(), "/job/1", nil, nil); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got := rec.get(0).Header.Get("Authorization"); got != "Bearer "+stale {
		t.Errorf("Authorization = %q, want the original token", got)
	}
}

func TestConcurrentRequests_ShareSingleLogin(t *testing.T) {
	srv, mux := newServer(t)
	te := newTokenEndpoint(validToken())
	mux.HandleFunc("POST /api/oauth2/token", te.handler(t))
	mux.HandleFunc("GET /api/job/1", dataResponse(map[string]any{"id": 1}))
	c := newTestClient(t, srv, WithToken(expiredToken()), WithClientCredentials("id", "secret"))

	const workers = 20
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Get(context.Background(), "/job/1", nil, nil); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("Get: %v", err)
	}
	if te.calls() != 1 {
		t.Errorf("token endpoint called %d times, want 1", te.calls())
	}
}

func TestAttach_MultipartAndRetryAfter401(t *testing.T) {
	srv, mux := newServer(t)
	oldToken := makeJWT(time.Now().Add(time.Hour))
	newToken := makeJWT(time.Now().Add(2 * time.Hour))
	te := newTokenEndpoint(oldToken, newToken)
	mux.HandleFunc("POST /api/oauth2/token", te.handler(t))

	type upload struct {
		contentType     string
		entityType      string
		entityID        string
		filename        string
		fileContentType string
		content         string
	}
	var (
		attempts atomic.Int32
		mu       sync.Mutex
		got      upload
	)
	mux.HandleFunc("POST /api/attachment", func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parsing multipart form: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f, hdr, err := r.FormFile(attachmentFieldName)
		if err != nil {
			t.Errorf("reading uploadedFile: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer f.Close()
		content, _ := io.ReadAll(f)
		mu.Lock()
		got = upload{
			contentType:     r.Header.Get("Content-Type"),
			entityType:      r.FormValue("entityType"),
			entityID:        r.FormValue("entityId"),
			filename:        hdr.Filename,
			fileContentType: hdr.Header.Get("Content-Type"),
			content:         string(content),
		}
		mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"id": 789}})
	})
	c := newTestClient(t, srv, WithClientCredentials("id", "secret"))

	var out struct {
		ID int `json:"id"`
	}
	_, err := c.Attach(
		context.Background(),
		url.Values{"entityType": {"3"}, "entityId": {"123"}},
		FileAttachment{Reader: strings.NewReader("hello"), Filename: "doc.pdf", ContentType: "application/pdf"},
		&out,
	)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if out.ID != 789 {
		t.Errorf("decoded id = %d, want 789", out.ID)
	}
	if attempts.Load() != 2 {
		t.Errorf("attachment endpoint called %d times, want 2", attempts.Load())
	}
	if te.calls() != 2 {
		t.Errorf("token endpoint called %d times, want 2", te.calls())
	}

	mu.Lock()
	defer mu.Unlock()
	if !strings.HasPrefix(got.contentType, "multipart/form-data") {
		t.Errorf("retry Content-Type = %q, want multipart/form-data", got.contentType)
	}
	want := upload{
		contentType:     got.contentType,
		entityType:      "3",
		entityID:        "123",
		filename:        "doc.pdf",
		fileContentType: "application/pdf",
		content:         "hello",
	}
	if got != want {
		t.Errorf("upload = %+v, want %+v", got, want)
	}
}

func TestCustomHeadersAndUserAgent(t *testing.T) {
	srv, mux := newServer(t)
	rec := &recorder{}
	mux.HandleFunc("GET /api/job/1", func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"id": 1}})
	})
	c := newTestClient(t, srv,
		WithToken(validToken()),
		WithUserAgent("My App/1.0"),
		WithHeader("X-Custom", "one"),
	)
	c.SetHeader("X-Other", "two")

	if _, err := c.Get(context.Background(), "/job/1", nil, nil); err != nil {
		t.Fatalf("Get: %v", err)
	}
	hdr := rec.get(0).Header
	for key, want := range map[string]string{
		"User-Agent": "My App/1.0",
		"X-Custom":   "one",
		"X-Other":    "two",
	} {
		if got := hdr.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestAPIError_ParsesStructuredMessages(t *testing.T) {
	srv, mux := newServer(t)
	mux.HandleFunc("GET /api/structured", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"messages": map[string]any{
			"error":      []string{"Bad thing", "Worse thing"},
			"validation": []string{"name is required"},
		}})
	})
	mux.HandleFunc("GET /api/single", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]any{"messages": map[string]any{"error": "Not found"}})
	})
	mux.HandleFunc("GET /api/plain", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("oops"))
	})
	c := newTestClient(t, srv, WithToken(validToken()), WithMaxRetries(0))
	ctx := context.Background()

	resp, err := c.Get(ctx, "/structured", nil, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("Get error = %v, want *APIError", err)
	}
	if resp == nil || resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Response alongside error = %+v, want status 400", resp)
	}
	if apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode = %d", apiErr.StatusCode)
	}
	if !slices.Equal(apiErr.Messages, []string{"Bad thing", "Worse thing"}) {
		t.Errorf("Messages = %v", apiErr.Messages)
	}
	if !slices.Equal(apiErr.Validation, []string{"name is required"}) {
		t.Errorf("Validation = %v", apiErr.Validation)
	}
	for _, want := range []string{"status 400", "Errors: Bad thing; Worse thing", "Validation: name is required"} {
		if !strings.Contains(apiErr.Error(), want) {
			t.Errorf("Error() = %q, want it to contain %q", apiErr.Error(), want)
		}
	}

	_, err = c.Get(ctx, "/single", nil, nil)
	if !errors.As(err, &apiErr) {
		t.Fatalf("Get error = %v, want *APIError", err)
	}
	if !slices.Equal(apiErr.Messages, []string{"Not found"}) || len(apiErr.Validation) != 0 {
		t.Errorf("single-string error parsed as %+v", apiErr)
	}

	_, err = c.Get(ctx, "/plain", nil, nil)
	if !errors.As(err, &apiErr) {
		t.Fatalf("Get error = %v, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusBadGateway || string(apiErr.Body) != "oops" || apiErr.Messages != nil {
		t.Errorf("plain error parsed as %+v", apiErr)
	}
	if !strings.Contains(apiErr.Error(), "status 502") {
		t.Errorf("Error() = %q", apiErr.Error())
	}
}

func TestRetries_IdempotentMethodsOnly(t *testing.T) {
	srv, mux := newServer(t)
	var gets, posts, downs atomic.Int32
	mux.HandleFunc("GET /api/flaky", func(w http.ResponseWriter, _ *http.Request) {
		if gets.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"ok": true}})
	})
	mux.HandleFunc("POST /api/flaky", func(w http.ResponseWriter, _ *http.Request) {
		posts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	mux.HandleFunc("GET /api/down", func(w http.ResponseWriter, _ *http.Request) {
		downs.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	})
	c := newTestClient(t, srv, WithToken(validToken()), WithMaxRetries(2))
	ctx := context.Background()

	if _, err := c.Get(ctx, "/flaky", nil, nil); err != nil {
		t.Fatalf("Get flaky: %v", err)
	}
	if gets.Load() != 3 {
		t.Errorf("GET attempted %d times, want 3", gets.Load())
	}

	var apiErr *APIError
	if _, err := c.Post(ctx, "/flaky", nil, map[string]any{}, nil); !errors.As(err, &apiErr) {
		t.Fatalf("Post flaky error = %v, want *APIError", err)
	}
	if posts.Load() != 1 {
		t.Errorf("POST attempted %d times, want 1 (never retried)", posts.Load())
	}

	if _, err := c.Get(ctx, "/down", nil, nil); !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusInternalServerError {
		t.Fatalf("Get down error = %v, want *APIError 500", err)
	}
	if downs.Load() != 3 {
		t.Errorf("GET attempted %d times, want 3 (1 + 2 retries)", downs.Load())
	}
}

func TestLogout(t *testing.T) {
	srv, mux := newServer(t)
	te := newTokenEndpoint(validToken())
	mux.HandleFunc("POST /api/oauth2/token", te.handler(t))
	var (
		mu     sync.Mutex
		revoke url.Values
	)
	mux.HandleFunc("POST /api/oauth2/revoke", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		mu.Lock()
		revoke = r.PostForm
		mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError)
	})
	unset := false
	c := newTestClient(t, srv, WithRefreshToken("rt-0"), WithOnUnsetAuth(func() { unset = true }))

	if _, err := c.Login(context.Background()); err != nil {
		t.Fatalf("Login: %v", err)
	}
	c.Logout(context.Background())

	if c.Token() != "" {
		t.Errorf("Token() = %q after Logout, want empty", c.Token())
	}
	if !unset {
		t.Error("OnUnsetAuth was not called")
	}
	mu.Lock()
	defer mu.Unlock()
	if got := revoke.Get("token"); got != "rt-0" {
		t.Errorf("revoke token = %q, want rt-0", got)
	}
}

func TestURLNormalization(t *testing.T) {
	srv, mux := newServer(t)
	rec := &recorder{}
	handler := func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"id": 1}})
	}
	mux.HandleFunc("GET /api/job/1", handler)
	mux.HandleFunc("GET /api/job", handler)
	c := newTestClient(t, srv,
		WithBaseURL(srv.URL+"/"),
		WithAPIPrefix("api/"),
		WithToken(validToken()),
	)
	ctx := context.Background()

	if _, err := c.Get(ctx, "job/1", nil, nil); err != nil {
		t.Fatalf("Get job/1: %v", err)
	}
	if got := rec.get(0).URL.RequestURI(); got != "/api/job/1" {
		t.Errorf("request URI = %q, want /api/job/1", got)
	}

	if _, err := c.Get(ctx, "/job?status=open", url.Values{"page": {"2"}}, nil); err != nil {
		t.Fatalf("Get /job: %v", err)
	}
	q := rec.get(1).URL.Query()
	if q.Get("status") != "open" || q.Get("page") != "2" {
		t.Errorf("query = %v, want status=open and page=2 merged", q)
	}
}
