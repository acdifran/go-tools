package servicetrade

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// ErrMissingCredentials is returned by NewClient when no credentials are
// configured, and wrapped in an *AuthError when a token-only client needs to
// log in.
var ErrMissingCredentials = errors.New(
	"servicetrade: client credentials, a refresh token, or a token are required",
)

// AuthError is returned when obtaining a bearer token fails.
type AuthError struct {
	// StatusCode is the HTTP status returned by the token endpoint, or 0 when
	// the request never completed.
	StatusCode int
	// Body is the raw response body, if any.
	Body []byte
	// Err is the underlying error, if any.
	Err error
}

func (e *AuthError) Error() string {
	switch {
	case e.Err != nil && e.StatusCode != 0:
		return fmt.Sprintf("servicetrade: authentication failed (status %d): %v", e.StatusCode, e.Err)
	case e.Err != nil:
		return fmt.Sprintf("servicetrade: authentication failed: %v", e.Err)
	default:
		return fmt.Sprintf("servicetrade: authentication failed (status %d)", e.StatusCode)
	}
}

func (e *AuthError) Unwrap() error { return e.Err }

// APIError is returned when the API responds with a non-2xx status.
type APIError struct {
	StatusCode int
	Header     http.Header
	// Body is the raw response body.
	Body []byte
	// Messages holds messages.error from a structured ServiceTrade error
	// response, if present.
	Messages []string
	// Validation holds messages.validation from a structured ServiceTrade
	// error response, if present.
	Validation []string
}

func (e *APIError) Error() string {
	var parts []string
	if len(e.Messages) > 0 {
		parts = append(parts, "Errors: "+strings.Join(e.Messages, "; "))
	}
	if len(e.Validation) > 0 {
		parts = append(parts, "Validation: "+strings.Join(e.Validation, "; "))
	}
	if len(parts) > 0 {
		return fmt.Sprintf("servicetrade: API request failed (status %d): %s",
			e.StatusCode, strings.Join(parts, " | "))
	}
	return fmt.Sprintf("servicetrade: API request failed (status %d)", e.StatusCode)
}

func newAPIError(resp *Response) *APIError {
	apiErr := &APIError{
		StatusCode: resp.StatusCode,
		Header:     resp.Header,
		Body:       resp.Body,
		Messages:   nil,
		Validation: nil,
	}
	var envelope struct {
		Messages struct {
			Error      stringList `json:"error"`
			Validation stringList `json:"validation"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(resp.Body, &envelope); err == nil {
		apiErr.Messages = envelope.Messages.Error
		apiErr.Validation = envelope.Messages.Validation
	}
	return apiErr
}

// stringList decodes a JSON string or array of values into []string, matching
// the lenient message parsing of the official SDKs. Anything else decodes to
// nil without error.
type stringList []string

func (s *stringList) UnmarshalJSON(b []byte) error {
	var single string
	if err := json.Unmarshal(b, &single); err == nil {
		*s = []string{single}
		return nil
	}
	var many []json.RawMessage
	if err := json.Unmarshal(b, &many); err != nil {
		*s = nil
		return nil
	}
	out := make([]string, 0, len(many))
	for _, raw := range many {
		var str string
		if err := json.Unmarshal(raw, &str); err == nil {
			out = append(out, str)
			continue
		}
		out = append(out, string(raw))
	}
	*s = out
	return nil
}
