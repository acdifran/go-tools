package middleware

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/acdifran/go-tools/membershiprole"
	"github.com/acdifran/go-tools/pulid"
	"github.com/acdifran/go-tools/viewer"
	"github.com/clerk/clerk-sdk-go/v2"
	clerkhttp "github.com/clerk/clerk-sdk-go/v2/http"

	clerktools "github.com/acdifran/go-tools/clerk"
)

func EnableCORS(clientURL string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", clientURL)

			if r.Method == "OPTIONS" {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
				w.Header().
					Set("Access-Control-Allow-Headers", "Accept, Content-Type, X-CSRF-Token, Authorization, Vc-Override-Id, Vc-Override-Org-Id")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func CookieAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authHeader := r.Header.Get("Authorization"); authHeader == "" {
			cookieToken, _ := r.Cookie("__session")
			if cookieToken != nil {
				r.Header.Set("Authorization", "Bearer "+cookieToken.Value)
			}
		}
		next.ServeHTTP(w, r)
	})
}

type CustomClaims struct {
	OrgID        string `json:"app_org_id"`
	OrgAccountID string `json:"org_id"`
	OrgRole      string `json:"org_role"`
	OrgSlug      string `json:"org_slug"`
	UserID       string `json:"app_user_id"`
	Role         string `json:"role"`
}

func createAuthViewerContext(
	claims *clerk.SessionClaims,
	vcOverrideID string,
	vcOverrideOrgID string,
) *viewer.Context {
	customClaims := claims.Custom.(*CustomClaims)
	if customClaims == nil {
		slog.Error("missing custom claims", "subjet", claims.Subject)
		return viewer.LoggedOutContext()
	}
	orgID := customClaims.OrgID

	var orgMembershipRole membershiprole.MembershipRole
	var err error
	if orgID != "" {
		orgMembershipRole, err = clerktools.ClerkRoleToMembershipRole(customClaims.OrgRole)
		if err != nil {
			slog.Error(err.Error())
			return viewer.LoggedOutContext()
		}
	}

	role := viewer.User
	if customClaims.Role == "EMPLOYEE" {
		role = viewer.Employee
	}

	if customClaims.UserID == "" {
		slog.Error("missing user ID in claims", "subjet", claims.Subject)
		return viewer.LoggedOutContext()
	}

	user := viewer.Context{
		Role:              role,
		ID:                pulid.ID(customClaims.UserID),
		OrgID:             pulid.ID(orgID),
		AccountID:         claims.Subject,
		OrgAccountID:      customClaims.OrgAccountID,
		OrgMembershipRole: orgMembershipRole,
	}

	if user.Role == viewer.Employee && vcOverrideID != "" {
		user = viewer.Context{
			ID:    pulid.ID(vcOverrideID),
			OrgID: pulid.ID(vcOverrideOrgID),
		}
	}

	return &user
}

func AuthenticateWithClerk(
	loggedOutVC func(ctx context.Context) context.Context,
	newContextFromBase func(ctx context.Context, base *viewer.Context) context.Context,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Upgrade") == "websocket" {
				next.ServeHTTP(w, r)
				return
			}

			ctx := r.Context()

			claims, ok := clerk.SessionClaimsFromContext(ctx)
			if !ok || claims == nil {
				// slog.Warn(
				// 	"request missing auth session claims",
				// 	"Authorization", r.Header.Get("Authorization"),
				// 	"URI", r.RequestURI,
				// 	"RemmoteAddr", r.RemoteAddr,
				// )

				next.ServeHTTP(w, r.WithContext(loggedOutVC(ctx)))
				return
			}

			authContext := createAuthViewerContext(
				claims,
				r.Header.Get("Vc-Override-Id"),
				r.Header.Get("Vc-Override-Org-Id"),
			)

			next.ServeHTTP(
				w,
				r.WithContext(newContextFromBase(ctx, authContext)),
			)
		})
	}
}

func WithCustomClaims() clerkhttp.AuthorizationOption {
	return func(params *clerkhttp.AuthorizationParams) error {
		params.VerifyParams.CustomClaimsConstructor = func(ctx context.Context) any {
			return &CustomClaims{}
		}
		return nil
	}
}

func UseIf(
	shouldRun func(r *http.Request) bool,
	mw func(http.Handler) http.Handler,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if shouldRun(r) {
				mw(next).ServeHTTP(w, r)
			} else {
				next.ServeHTTP(w, r)
			}
		})
	}
}

func SkipIf(
	shouldSkip func(r *http.Request) bool,
	mw func(http.Handler) http.Handler,
) func(http.Handler) http.Handler {
	return UseIf(func(r *http.Request) bool {
		return !shouldSkip(r)
	}, mw)
}

func Chain(middlewares ...func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(final http.Handler) http.Handler {
		for i := len(middlewares) - 1; i >= 0; i-- {
			final = middlewares[i](final)
		}
		return final
	}
}

func SetManualViewer(
	newContextFromBase func(ctx context.Context, base *viewer.Context) context.Context,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			authContext := &viewer.Context{
				ID:    pulid.ID(r.Header.Get("Vc-Override-Id")),
				OrgID: pulid.ID(r.Header.Get("Vc-Override-Org-Id")),
			}
			next.ServeHTTP(
				w,
				r.WithContext(newContextFromBase(ctx, authContext)),
			)
		})
	}
}

func IsViewerOverrideSet(r *http.Request) bool {
	return r.Header.Get("Vc-Override-Id") != ""
}
