package middleware

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/acdifran/go-tools/clerkhooks"
	"github.com/acdifran/go-tools/viewer"
	"github.com/clerk/clerk-sdk-go/v2"
	clerkjwt "github.com/clerk/clerk-sdk-go/v2/jwt"
	"github.com/clerk/clerk-sdk-go/v2/organizationmembership"
	"github.com/clerk/clerk-sdk-go/v2/session"
)

// The socket's viewer is fixed at connect, so it is only as current as this interval: a signed-out session or a
// removed member keeps the open socket until the next check closes it.
var websocketRecheckInterval = 5 * time.Minute

var errWebsocketAuthRevoked = errors.New("websocket session no longer authorized")

func WebsocketInit(
	clerkhook *clerkhooks.ClerkHook,
) transport.WebsocketInitFunc {
	return func(
		ctx context.Context,
		initPayload transport.InitPayload,
	) (context.Context, *transport.InitPayload, error) {
		token := initPayload.Authorization()
		loggedOut := viewer.LoggedOutContext(ctx)

		if token == "" {
			return loggedOut, &initPayload, nil
		}

		claims, err := clerkjwt.Verify(ctx, &clerkjwt.VerifyParams{
			Token: token,
			CustomClaimsConstructor: func(ctx context.Context) any {
				return &CustomClaims{}
			},
		})
		if err != nil {
			return loggedOut, &initPayload, nil
		}

		authContext := createAuthViewerContext(
			ctx,
			claims,
			"",
			"",
			clerkhook,
		)
		if authContext.IsLoggedOut() {
			return loggedOut, &initPayload, nil
		}

		// gqlgen closes the socket when this context ends, and the client reconnects with a fresh token.
		ctx, cancel := context.WithCancelCause(ctx)
		go watchWebsocketAuth(ctx, cancel, claims)
		return viewer.NewContext(ctx, authContext), &initPayload, nil
	}
}

func watchWebsocketAuth(
	ctx context.Context,
	cancel context.CancelCauseFunc,
	claims *clerk.SessionClaims,
) {
	ticker := time.NewTicker(websocketRecheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			err := websocketStillAuthorized(ctx, claims)
			if errors.Is(err, errWebsocketAuthRevoked) {
				slog.Info("closing websocket", "reason", err, "session", claims.SessionID)
				cancel(err)
				return
			}
			// A Clerk outage shouldn't drop every socket; the next tick checks again.
			if err != nil && ctx.Err() == nil {
				slog.Warn("rechecking websocket auth", "error", err, "session", claims.SessionID)
			}
		}
	}
}

func websocketStillAuthorized(ctx context.Context, claims *clerk.SessionClaims) error {
	sess, err := session.Get(ctx, claims.SessionID)
	if isClerkNotFound(err) {
		return fmt.Errorf("%w: session %s not found", errWebsocketAuthRevoked, claims.SessionID)
	}
	if err != nil {
		return fmt.Errorf("getting clerk session: %w", err)
	}
	if sess.Status != "active" {
		return fmt.Errorf("%w: session is %s", errWebsocketAuthRevoked, sess.Status)
	}

	if claims.ActiveOrganizationID == "" {
		return nil
	}
	memberships, err := organizationmembership.List(ctx, &organizationmembership.ListParams{
		OrganizationID: claims.ActiveOrganizationID,
		UserIDs:        []string{claims.Subject},
	})
	if isClerkNotFound(err) {
		return fmt.Errorf(
			"%w: organization %s not found",
			errWebsocketAuthRevoked,
			claims.ActiveOrganizationID,
		)
	}
	if err != nil {
		return fmt.Errorf("listing clerk organization memberships: %w", err)
	}
	if len(memberships.OrganizationMemberships) == 0 {
		return fmt.Errorf("%w: no longer an organization member", errWebsocketAuthRevoked)
	}
	// The viewer's role came from the token, so a changed role needs a reconnect to take effect.
	if role := memberships.OrganizationMemberships[0].Role; role != claims.ActiveOrganizationRole {
		return fmt.Errorf(
			"%w: organization role changed from %s to %s",
			errWebsocketAuthRevoked,
			claims.ActiveOrganizationRole,
			role,
		)
	}
	return nil
}

func isClerkNotFound(err error) bool {
	var apiErr *clerk.APIErrorResponse
	return errors.As(err, &apiErr) && apiErr.HTTPStatusCode == http.StatusNotFound
}
