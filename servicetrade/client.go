package servicetrade

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultBaseURL is the production ServiceTrade API host.
	DefaultBaseURL = "https://api.servicetrade.com"
	// DefaultAPIPrefix is the path under which the API is served.
	DefaultAPIPrefix = "/api"
	// DefaultUserAgent is sent when no user agent is configured.
	DefaultUserAgent = "ServiceTrade Go SDK"

	// tokenTTLBuffer is how long before its expiry a token is treated as stale.
	tokenTTLBuffer = 5 * time.Minute

	defaultTimeout      = 60 * time.Second
	defaultMaxRetries   = 3
	defaultRetryBackoff = 500 * time.Millisecond
)

type grantType string

const (
	grantClientCredentials grantType = "client_credentials"
	grantRefreshToken      grantType = "refresh_token"
)

// credentials are the OAuth2 grant details used to obtain bearer tokens.
type credentials struct {
	grantType    grantType
	clientID     string
	clientSecret string
	refreshToken string
}

func (c *credentials) form() url.Values {
	form := url.Values{}
	form.Set("grant_type", string(c.grantType))
	switch c.grantType {
	case grantRefreshToken:
		form.Set("refresh_token", c.refreshToken)
	case grantClientCredentials:
		form.Set("client_id", c.clientID)
		form.Set("client_secret", c.clientSecret)
	}
	return form
}

type config struct {
	baseURL         string
	apiPrefix       string
	userAgent       string
	httpClient      *http.Client
	autoRefreshAuth bool
	maxRetries      int
	clientID        string
	clientSecret    string
	refreshToken    string
	token           string
	onSetAuth       func(token string)
	onUnsetAuth     func()
	headers         map[string]string
}

// Option configures a Client.
type Option func(*config)

// WithBaseURL sets the API host. Defaults to DefaultBaseURL.
func WithBaseURL(baseURL string) Option {
	return func(c *config) { c.baseURL = baseURL }
}

// WithAPIPrefix sets the path prefix of the API. Defaults to DefaultAPIPrefix.
func WithAPIPrefix(prefix string) Option {
	return func(c *config) { c.apiPrefix = prefix }
}

// WithUserAgent sets the User-Agent header. Defaults to DefaultUserAgent.
func WithUserAgent(userAgent string) Option {
	return func(c *config) { c.userAgent = userAgent }
}

// WithHTTPClient sets the underlying HTTP client. Defaults to a client with a
// 60 second timeout; use a context deadline to bound individual requests.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *config) { c.httpClient = httpClient }
}

// WithAutoRefreshAuth controls whether a stale token is refreshed before a
// request and whether a 401 response triggers a re-login and one retry.
// Enabled by default. It has no effect without refreshable credentials.
func WithAutoRefreshAuth(enabled bool) Option {
	return func(c *config) { c.autoRefreshAuth = enabled }
}

// WithMaxRetries sets how many times GET, PUT and DELETE requests are retried
// after a transport error or a 500, 502, 503 or 504 response. Defaults to 3.
// POST requests are never retried.
func WithMaxRetries(n int) Option {
	return func(c *config) { c.maxRetries = n }
}

// WithClientCredentials authenticates with the OAuth2 client credentials
// grant, for service-to-service integrations.
func WithClientCredentials(clientID, clientSecret string) Option {
	return func(c *config) {
		c.clientID = clientID
		c.clientSecret = clientSecret
	}
}

// WithRefreshToken authenticates with the OAuth2 refresh token grant. When the
// server rotates the refresh token the new value replaces it; read it back
// with Client.RefreshToken to persist it. Takes priority over client
// credentials when both are configured.
func WithRefreshToken(refreshToken string) Option {
	return func(c *config) { c.refreshToken = refreshToken }
}

// WithToken uses an existing bearer token. Combine it with
// WithClientCredentials or WithRefreshToken so the token can be refreshed
// once it expires.
func WithToken(token string) Option {
	return func(c *config) { c.token = token }
}

// WithOnSetAuth registers a callback invoked with each newly obtained token.
func WithOnSetAuth(fn func(token string)) Option {
	return func(c *config) { c.onSetAuth = fn }
}

// WithOnUnsetAuth registers a callback invoked by Logout.
func WithOnUnsetAuth(fn func()) Option {
	return func(c *config) { c.onUnsetAuth = fn }
}

// WithHeader adds a header sent with every request.
func WithHeader(key, value string) Option {
	return func(c *config) { c.headers[key] = value }
}

// Client is a ServiceTrade API client. It is safe for concurrent use: token
// refreshes are serialized so concurrent callers share a single login.
type Client struct {
	apiURL          string
	userAgent       string
	httpClient      *http.Client
	autoRefreshAuth bool
	maxRetries      int
	retryBackoff    time.Duration
	onSetAuth       func(token string)
	onUnsetAuth     func()

	mu           sync.Mutex
	headers      map[string]string
	creds        *credentials
	token        string
	tokenExpiry  time.Time
	expiryParsed bool
}

// NewClient builds a Client from the given options. It returns
// ErrMissingCredentials unless client credentials, a refresh token, or a
// bearer token is configured.
func NewClient(opts ...Option) (*Client, error) {
	cfg := config{
		baseURL:         DefaultBaseURL,
		apiPrefix:       DefaultAPIPrefix,
		userAgent:       DefaultUserAgent,
		httpClient:      nil,
		autoRefreshAuth: true,
		maxRetries:      defaultMaxRetries,
		clientID:        "",
		clientSecret:    "",
		refreshToken:    "",
		token:           "",
		onSetAuth:       nil,
		onUnsetAuth:     nil,
		headers:         map[string]string{},
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	var creds *credentials
	switch {
	case cfg.refreshToken != "":
		creds = &credentials{
			grantType:    grantRefreshToken,
			clientID:     "",
			clientSecret: "",
			refreshToken: cfg.refreshToken,
		}
	case cfg.clientID != "" && cfg.clientSecret != "":
		creds = &credentials{
			grantType:    grantClientCredentials,
			clientID:     cfg.clientID,
			clientSecret: cfg.clientSecret,
			refreshToken: "",
		}
	}
	if creds == nil && cfg.token == "" {
		return nil, ErrMissingCredentials
	}

	baseURL := strings.TrimRight(cfg.baseURL, "/")
	apiPrefix := cfg.apiPrefix
	if apiPrefix != "" && !strings.HasPrefix(apiPrefix, "/") {
		apiPrefix = "/" + apiPrefix
	}
	apiPrefix = strings.TrimRight(apiPrefix, "/")

	httpClient := cfg.httpClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}

	return &Client{
		apiURL:          baseURL + apiPrefix,
		userAgent:       cfg.userAgent,
		httpClient:      httpClient,
		autoRefreshAuth: cfg.autoRefreshAuth,
		maxRetries:      max(cfg.maxRetries, 0),
		retryBackoff:    defaultRetryBackoff,
		onSetAuth:       cfg.onSetAuth,
		onUnsetAuth:     cfg.onUnsetAuth,
		mu:              sync.Mutex{},
		headers:         cfg.headers,
		creds:           creds,
		token:           cfg.token,
		tokenExpiry:     time.Time{},
		expiryParsed:    false,
	}, nil
}

// Token returns the current bearer token, or "" before the first login.
func (c *Client) Token() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.token
}

// RefreshToken returns the current refresh token, which differs from the one
// given to WithRefreshToken once the server has rotated it. It is "" when the
// client does not use the refresh token grant.
func (c *Client) RefreshToken() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.creds == nil {
		return ""
	}
	return c.creds.refreshToken
}

// SetHeader sets a header sent with every subsequent request.
func (c *Client) SetHeader(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.headers[key] = value
}

// Login authenticates with the configured credentials and stores the new
// bearer token. Requests log in lazily, so calling Login is optional. For a
// token-only client it returns an *AuthError wrapping ErrMissingCredentials.
func (c *Client) Login(ctx context.Context) (string, error) {
	token, err := func() (string, error) {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.loginLocked(ctx)
	}()
	if err != nil {
		return "", err
	}
	c.notifySetAuth(token)
	return token, nil
}

// Logout discards the bearer token, revokes the refresh token on a best-effort
// basis and invokes the OnUnsetAuth callback. Revocation failures are ignored.
func (c *Client) Logout(ctx context.Context) {
	c.mu.Lock()
	c.setTokenLocked("")
	refreshToken := ""
	if c.creds != nil {
		refreshToken = c.creds.refreshToken
	}
	c.mu.Unlock()

	if refreshToken != "" {
		c.revoke(ctx, refreshToken)
	}
	if c.onUnsetAuth != nil {
		c.onUnsetAuth()
	}
}

func (c *Client) revoke(ctx context.Context, refreshToken string) {
	req, err := c.newAuthRequest(ctx, "/oauth2/revoke", url.Values{"token": {refreshToken}})
	if err != nil {
		return
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}

func (c *Client) newAuthRequest(ctx context.Context, path string, form url.Values) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL+path, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req, nil
}

// loginLocked requests a token with the configured grant. c.mu must be held.
func (c *Client) loginLocked(ctx context.Context) (string, error) {
	if c.creds == nil {
		return "", &AuthError{StatusCode: 0, Body: nil, Err: ErrMissingCredentials}
	}
	req, err := c.newAuthRequest(ctx, "/oauth2/token", c.creds.form())
	if err != nil {
		return "", &AuthError{StatusCode: 0, Body: nil, Err: err}
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", &AuthError{StatusCode: 0, Body: nil, Err: err}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", &AuthError{StatusCode: resp.StatusCode, Body: nil, Err: err}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", &AuthError{StatusCode: resp.StatusCode, Body: body, Err: nil}
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", &AuthError{
			StatusCode: resp.StatusCode,
			Body:       body,
			Err:        fmt.Errorf("decoding token response: %w", err),
		}
	}
	if tokenResp.AccessToken == "" {
		return "", &AuthError{
			StatusCode: resp.StatusCode,
			Body:       body,
			Err:        errors.New("missing access_token in token response"),
		}
	}

	c.setTokenLocked(tokenResp.AccessToken)
	if tokenResp.RefreshToken != "" && c.creds.grantType == grantRefreshToken {
		c.creds.refreshToken = tokenResp.RefreshToken
	}
	return tokenResp.AccessToken, nil
}

func (c *Client) notifySetAuth(token string) {
	if c.onSetAuth != nil {
		c.onSetAuth(token)
	}
}

// ensureToken returns a usable bearer token, logging in when there is none or
// when auto-refresh is enabled and the current token is stale.
func (c *Client) ensureToken(ctx context.Context) (string, error) {
	token, loggedIn, err := func() (string, bool, error) {
		c.mu.Lock()
		defer c.mu.Unlock()
		needsLogin := c.token == "" || (c.autoRefreshAuth && c.creds != nil && c.staleLocked())
		if !needsLogin {
			return c.token, false, nil
		}
		token, err := c.loginLocked(ctx)
		return token, true, err
	}()
	if err != nil {
		return "", err
	}
	if loggedIn {
		c.notifySetAuth(token)
	}
	return token, nil
}

// refreshAfterUnauthorized obtains a fresh token after a 401. If another
// caller has already replaced usedToken, that token is reused instead of
// logging in again.
func (c *Client) refreshAfterUnauthorized(ctx context.Context, usedToken string) (string, error) {
	token, refreshed, err := func() (string, bool, error) {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.token != "" && c.token != usedToken {
			return c.token, false, nil
		}
		token, err := c.loginLocked(ctx)
		return token, true, err
	}()
	if err != nil {
		return "", err
	}
	if refreshed {
		c.notifySetAuth(token)
	}
	return token, nil
}

func (c *Client) setTokenLocked(token string) {
	c.token = token
	c.tokenExpiry = time.Time{}
	c.expiryParsed = false
}

// staleLocked reports whether the current token has expired or will within
// tokenTTLBuffer. A token whose expiry cannot be determined is never stale.
func (c *Client) staleLocked() bool {
	if c.token == "" {
		return true
	}
	if !c.expiryParsed {
		c.tokenExpiry = parseTokenExpiry(c.token)
		c.expiryParsed = true
	}
	if c.tokenExpiry.IsZero() {
		return false
	}
	return !time.Now().Before(c.tokenExpiry.Add(-tokenTTLBuffer))
}

// parseTokenExpiry reads the exp claim of a JWT without verifying it. It
// returns the zero time when the token is not a JWT or has no exp claim.
func parseTokenExpiry(token string) time.Time {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return time.Time{}
	}
	var claims struct {
		Exp *float64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp == nil {
		return time.Time{}
	}
	return time.Unix(int64(*claims.Exp), 0)
}
