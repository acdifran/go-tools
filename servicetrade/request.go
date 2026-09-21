package servicetrade

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"time"
)

// Response is the raw HTTP response of a completed API request.
type Response struct {
	StatusCode int
	Header     http.Header
	// Body is the complete, unmodified response body. The out argument of the
	// request methods receives the decoded "data" envelope instead.
	Body []byte
}

// FileAttachment is a file to upload with Attach.
type FileAttachment struct {
	// Reader supplies the file contents. It is read fully before the request
	// is sent so the upload can be retried after a token refresh.
	Reader io.Reader
	// Filename is sent as the multipart filename.
	Filename string
	// ContentType is the MIME type of the file. Defaults to
	// application/octet-stream when empty.
	ContentType string
}

const attachmentFieldName = "uploadedFile"

// Get performs a GET request. params are added to the query string. When out
// is non-nil the response is decoded into it.
func (c *Client) Get(ctx context.Context, path string, params url.Values, out any) (*Response, error) {
	return c.Do(ctx, http.MethodGet, path, params, nil, out)
}

// Post performs a POST request with body encoded as JSON.
func (c *Client) Post(ctx context.Context, path string, params url.Values, body, out any) (*Response, error) {
	return c.Do(ctx, http.MethodPost, path, params, body, out)
}

// Put performs a PUT request with body encoded as JSON.
func (c *Client) Put(ctx context.Context, path string, params url.Values, body, out any) (*Response, error) {
	return c.Do(ctx, http.MethodPut, path, params, body, out)
}

// Delete performs a DELETE request. The response body is not decoded.
func (c *Client) Delete(ctx context.Context, path string, params url.Values) (*Response, error) {
	return c.Do(ctx, http.MethodDelete, path, params, nil, nil)
}

// Do performs a request with an arbitrary method. path is relative to the API
// prefix and may carry its own query string, which params are merged into.
// A non-nil body is encoded as JSON. When out is non-nil and the response has
// a body, the "data" envelope (or the whole body when there is none) is
// decoded into out.
//
// A non-2xx status returns the Response together with an *APIError. When
// auto-refresh is enabled and refreshable credentials are configured, a 401
// triggers a re-login and a single retry before failing.
func (c *Client) Do(
	ctx context.Context,
	method, path string,
	params url.Values,
	body, out any,
) (*Response, error) {
	var payload []byte
	contentType := ""
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("servicetrade: encoding request body: %w", err)
		}
		payload = encoded
		contentType = "application/json"
	}
	return c.do(ctx, method, path, params, payload, contentType, out)
}

// Attach uploads a file to POST /attachment. fields are sent as the multipart
// form fields accompanying the file, for example entityType, entityId and
// purposeId.
func (c *Client) Attach(
	ctx context.Context,
	fields url.Values,
	file FileAttachment,
	out any,
) (*Response, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for key, values := range fields {
		for _, value := range values {
			if err := mw.WriteField(key, value); err != nil {
				return nil, fmt.Errorf("servicetrade: writing multipart field %q: %w", key, err)
			}
		}
	}
	part, err := createFilePart(mw, file)
	if err != nil {
		return nil, fmt.Errorf("servicetrade: creating multipart file part: %w", err)
	}
	if file.Reader != nil {
		if _, err := io.Copy(part, file.Reader); err != nil {
			return nil, fmt.Errorf("servicetrade: reading attachment %q: %w", file.Filename, err)
		}
	}
	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("servicetrade: finalizing multipart body: %w", err)
	}
	return c.do(ctx, http.MethodPost, "/attachment", nil, buf.Bytes(), mw.FormDataContentType(), out)
}

var quoteEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

func createFilePart(mw *multipart.Writer, file FileAttachment) (io.Writer, error) {
	if file.ContentType == "" {
		return mw.CreateFormFile(attachmentFieldName, file.Filename)
	}
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`,
		attachmentFieldName, quoteEscaper.Replace(file.Filename)))
	header.Set("Content-Type", file.ContentType)
	return mw.CreatePart(header)
}

func (c *Client) do(
	ctx context.Context,
	method, path string,
	params url.Values,
	payload []byte,
	contentType string,
	out any,
) (*Response, error) {
	token, err := c.ensureToken(ctx)
	if err != nil {
		return nil, err
	}
	target, err := c.buildURL(path, params)
	if err != nil {
		return nil, err
	}

	resp, err := c.send(ctx, method, target, payload, contentType, token)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized && c.autoRefreshAuth && c.creds != nil {
		token, err = c.refreshAfterUnauthorized(ctx, token)
		if err != nil {
			return nil, err
		}
		resp, err = c.send(ctx, method, target, payload, contentType, token)
		if err != nil {
			return nil, err
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp, newAPIError(resp)
	}
	if out != nil && len(resp.Body) > 0 {
		if err := decodeBody(resp.Body, out); err != nil {
			return resp, fmt.Errorf("servicetrade: decoding response: %w", err)
		}
	}
	return resp, nil
}

// send performs one logical request, retrying idempotent methods on transport
// errors and transient server statuses with exponential backoff.
func (c *Client) send(
	ctx context.Context,
	method, target string,
	payload []byte,
	contentType, token string,
) (*Response, error) {
	headers := c.requestHeaders(token, contentType)
	attempts := 1
	if isIdempotent(method) {
		attempts += c.maxRetries
	}

	var (
		lastResp *Response
		lastErr  error
	)
	for attempt := range attempts {
		if attempt > 0 {
			if err := sleepCtx(ctx, c.retryBackoff<<(attempt-1)); err != nil {
				return nil, err
			}
		}
		req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("servicetrade: building request: %w", err)
		}
		req.Header = headers.Clone()

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("servicetrade: request failed: %w", err)
			}
			lastResp, lastErr = nil, err
			continue
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			lastResp, lastErr = nil, err
			continue
		}
		lastResp, lastErr = &Response{StatusCode: resp.StatusCode, Header: resp.Header, Body: body}, nil
		if !isRetryableStatus(resp.StatusCode) {
			return lastResp, nil
		}
	}
	if lastResp != nil {
		return lastResp, nil
	}
	return nil, fmt.Errorf("servicetrade: request failed: %w", lastErr)
}

func (c *Client) buildURL(path string, params url.Values) (string, error) {
	target, err := url.Parse(c.apiURL + "/" + strings.TrimLeft(path, "/"))
	if err != nil {
		return "", fmt.Errorf("servicetrade: invalid path %q: %w", path, err)
	}
	if len(params) > 0 {
		query := target.Query()
		for key, values := range params {
			for _, value := range values {
				query.Add(key, value)
			}
		}
		target.RawQuery = query.Encode()
	}
	return target.String(), nil
}

// requestHeaders builds the headers for a request: the user agent and content
// type, then any custom headers (which may override them), then the bearer
// token.
func (c *Client) requestHeaders(token, contentType string) http.Header {
	headers := http.Header{}
	headers.Set("User-Agent", c.userAgent)
	if contentType != "" {
		headers.Set("Content-Type", contentType)
	}
	c.mu.Lock()
	for key, value := range c.headers {
		headers.Set(key, value)
	}
	c.mu.Unlock()
	if token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}
	return headers
}

// decodeBody decodes a response body into out, unwrapping the "data" envelope
// when the body is an object that has one.
func decodeBody(body []byte, out any) error {
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && envelope.Data != nil {
		return json.Unmarshal(envelope.Data, out)
	}
	return json.Unmarshal(body, out)
}

func isIdempotent(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete, http.MethodOptions:
		return true
	default:
		return false
	}
}

func isRetryableStatus(status int) bool {
	switch status {
	case http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
