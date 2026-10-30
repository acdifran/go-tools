package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
)

type fakeClerk struct {
	sessionStatus int
	session       map[string]any
	memberships   []any
}

func serveFakeClerk(t *testing.T, f fakeClerk) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/sessions/sess_1":
			if f.sessionStatus != 0 {
				w.WriteHeader(f.sessionStatus)
				_ = json.NewEncoder(w).Encode(map[string]any{"errors": []any{}})
				return
			}
			_ = json.NewEncoder(w).Encode(f.session)
		case "/v1/organizations/org_1/memberships":
			if got := r.URL.Query().Get("user_id"); got != "user_1" {
				t.Errorf("memberships queried for user %q, want user_1", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data":        f.memberships,
				"total_count": len(f.memberships),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	url := srv.URL + "/v1"
	clerk.SetBackend(clerk.NewBackend(&clerk.BackendConfig{
		HTTPClient: srv.Client(),
		URL:        &url,
		Key:        nil,
	}))
}

func socketClaims(orgID, orgRole string) *clerk.SessionClaims {
	var c clerk.SessionClaims
	c.Subject = "user_1"
	c.SessionID = "sess_1"
	c.ActiveOrganizationID = orgID
	c.ActiveOrganizationRole = orgRole
	return &c
}

func clerkSession(status string) map[string]any {
	return map[string]any{"object": "session", "id": "sess_1", "status": status}
}

func membership(role string) []any {
	return []any{map[string]any{"object": "organization_membership", "role": role}}
}

func TestWebsocketStillAuthorized(t *testing.T) {
	for _, tc := range []struct {
		name        string
		clerk       fakeClerk
		claims      *clerk.SessionClaims
		wantRevoked bool
		wantErr     bool
	}{
		{
			name:        "active session and unchanged membership",
			clerk:       fakeClerk{sessionStatus: 0, session: clerkSession("active"), memberships: membership("org:admin")},
			claims:      socketClaims("org_1", "org:admin"),
			wantRevoked: false,
			wantErr:     false,
		},
		{
			name:        "personal org checks only the session",
			clerk:       fakeClerk{sessionStatus: 0, session: clerkSession("active"), memberships: nil},
			claims:      socketClaims("", ""),
			wantRevoked: false,
			wantErr:     false,
		},
		{
			name:        "revoked session",
			clerk:       fakeClerk{sessionStatus: 0, session: clerkSession("revoked"), memberships: membership("org:admin")},
			claims:      socketClaims("org_1", "org:admin"),
			wantRevoked: true,
			wantErr:     false,
		},
		{
			name:        "deleted session",
			clerk:       fakeClerk{sessionStatus: http.StatusNotFound, session: nil, memberships: nil},
			claims:      socketClaims("org_1", "org:admin"),
			wantRevoked: true,
			wantErr:     false,
		},
		{
			name:        "removed from the org",
			clerk:       fakeClerk{sessionStatus: 0, session: clerkSession("active"), memberships: nil},
			claims:      socketClaims("org_1", "org:admin"),
			wantRevoked: true,
			wantErr:     false,
		},
		{
			name:        "org role changed",
			clerk:       fakeClerk{sessionStatus: 0, session: clerkSession("active"), memberships: membership("org:member")},
			claims:      socketClaims("org_1", "org:admin"),
			wantRevoked: true,
			wantErr:     false,
		},
		{
			name:        "clerk outage is not a revocation",
			clerk:       fakeClerk{sessionStatus: http.StatusInternalServerError, session: nil, memberships: nil},
			claims:      socketClaims("org_1", "org:admin"),
			wantRevoked: false,
			wantErr:     true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			serveFakeClerk(t, tc.clerk)
			err := websocketStillAuthorized(t.Context(), tc.claims)
			if got := errors.Is(err, errWebsocketAuthRevoked); got != tc.wantRevoked {
				t.Errorf("revoked = %v (err %v), want %v", got, err, tc.wantRevoked)
			}
			if got := err != nil; got != (tc.wantRevoked || tc.wantErr) {
				t.Errorf("err = %v, want error %v", err, tc.wantRevoked || tc.wantErr)
			}
		})
	}
}

func TestWatchWebsocketAuthClosesRevokedSocket(t *testing.T) {
	defer func(d time.Duration) { websocketRecheckInterval = d }(websocketRecheckInterval)
	websocketRecheckInterval = 10 * time.Millisecond

	serveFakeClerk(t, fakeClerk{sessionStatus: 0, session: clerkSession("ended"), memberships: nil})
	ctx, cancel := context.WithCancelCause(t.Context())
	go watchWebsocketAuth(ctx, cancel, socketClaims("", ""))

	select {
	case <-ctx.Done():
		if !errors.Is(context.Cause(ctx), errWebsocketAuthRevoked) {
			t.Errorf("cause = %v, want a revocation", context.Cause(ctx))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("socket context was not cancelled")
	}
}

func TestWatchWebsocketAuthKeepsSocketThroughOutage(t *testing.T) {
	defer func(d time.Duration) { websocketRecheckInterval = d }(websocketRecheckInterval)
	websocketRecheckInterval = 10 * time.Millisecond

	serveFakeClerk(
		t,
		fakeClerk{sessionStatus: http.StatusInternalServerError, session: nil, memberships: nil},
	)
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)
	go watchWebsocketAuth(ctx, cancel, socketClaims("", ""))

	time.Sleep(100 * time.Millisecond)
	if err := ctx.Err(); err != nil {
		t.Errorf("socket context ended during an outage: %v", context.Cause(ctx))
	}
}
