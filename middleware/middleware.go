package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/acdifran/go-tools/clerkhooks"
	"github.com/samber/lo"

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
	UserID        string `json:"app_user_id"`
	Role          string `json:"app_user_role"`
	OrgID         string `json:"app_org_id"`
	PersonalOrgID string `json:"app_personal_org_id"`
}

func createAuthViewerContext(
	claims *clerk.SessionClaims,
	vcOverrideID string,
	vcOverrideOrgID string,
) *viewer.Context {
	customClaims, ok := claims.Custom.(*CustomClaims)
	if !ok {
		slog.Error("missing custom claims", "subject", claims.Subject)
		return viewer.LoggedOutContext()
	}

	if customClaims.UserID == "" {
		slog.Error("missing user ID in claims", "subject", claims.Subject)
		return viewer.LoggedOutContext()
	}

	orgID := customClaims.OrgID

	var orgMembershipRole membershiprole.MembershipRole
	var orgAccountID string
	var err error
	if orgID != "" {
		orgAccountID = claims.ActiveOrganizationID
		orgMembershipRole, err = clerktools.ClerkRoleToMembershipRole(
			claims.ActiveOrganizationRole,
		)
		if err != nil {
			slog.Error(
				"invalid organization role in claims",
				"role",
				claims.ActiveOrganizationRole,
				"error",
				err,
			)
			return viewer.LoggedOutContext()
		}
	}

	role := lo.Ternary(customClaims.Role == "EMPLOYEE", viewer.Employee, viewer.User)

	if orgID == "" && customClaims.PersonalOrgID != "" {
		orgID = customClaims.PersonalOrgID
		orgAccountID = claims.Subject
		orgMembershipRole = membershiprole.Admin
	}

	user := viewer.Context{
		Role:              role,
		ID:                pulid.ID(customClaims.UserID),
		OrgID:             pulid.ID(orgID),
		AccountID:         claims.Subject,
		OrgAccountID:      orgAccountID,
		OrgMembershipRole: orgMembershipRole,
	}

	if user.Role == viewer.Employee && vcOverrideID != "" {
		user = viewer.Context{
			ID:                pulid.ID(vcOverrideID),
			OrgID:             pulid.ID(vcOverrideOrgID),
			AccountID:         "",
			OrgAccountID:      "",
			OrgMembershipRole: membershiprole.Admin,
			Role:              viewer.Employee,
		}
	}

	return &user
}

func getOrCreateUserAndWriteCustomClaims(
	ctx context.Context,
	hook *clerkhooks.ClerkHook,
	claims *clerk.SessionClaims,
) (*clerk.SessionClaims, error) {
	customClaims, ok := claims.Custom.(*CustomClaims)
	if !ok {
		return nil, fmt.Errorf("missing custom claims for subject: %s", claims.Subject)
	}

	userID := pulid.Ptr(customClaims.UserID)
	personalOrgID := pulid.Ptr(customClaims.PersonalOrgID)

	user, err := hook.GetOrCreateUserByAccountID(
		ctx,
		claims.Subject,
		userID,
		personalOrgID,
		true,
	)
	if err != nil {
		return nil, fmt.Errorf("creating or fetching user: %w", err)
	}

	orgAccountID := claims.ActiveOrganizationID
	// not a personal org
	if orgAccountID != "" {
		orgID := pulid.Ptr(customClaims.OrgID)

		org, err := hook.GetOrCreateOrgByAccountID(ctx, orgAccountID, user.ID, orgID)
		if err != nil {
			return nil, fmt.Errorf("creating or fetching org: %w", err)
		}

		customClaims = &CustomClaims{
			UserID:        string(user.ID),
			Role:          "EMPLOYEE",
			OrgID:         string(org.ID),
			PersonalOrgID: "",
		}

		claims.Custom = customClaims
		return claims, nil
	}

	// is a personal org
	orgAccountID = claims.Subject
	org, err := hook.GetPersonalOrgByAccountID(ctx, orgAccountID, user, personalOrgID)
	if err != nil {
		return nil, fmt.Errorf("fetching personal org: %w", err)
	}

	customClaims = &CustomClaims{
		UserID:        string(user.ID),
		Role:          "EMPLOYEE",
		OrgID:         "",
		PersonalOrgID: string(org.ID),
	}
	claims.Custom = customClaims
	return claims, nil
}

func WriteClerkSessionClaimsFromLocalDB(
	hook *clerkhooks.ClerkHook,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			claims, ok := clerk.SessionClaimsFromContext(ctx)
			if !ok || claims == nil {
				fmt.Println("No claims found in context")
				next.ServeHTTP(w, r)
				return
			}

			apCtx := viewer.AllPowerfulVC(ctx)
			var err error
			claims, err = getOrCreateUserAndWriteCustomClaims(
				apCtx,
				hook,
				claims,
			)
			if err != nil {
				slog.Error("error in WriteClerkSessionClaimsFromLocalDB", "error", err)
				next.ServeHTTP(w, r)
				return
			}

			newCtx := clerk.ContextWithSessionClaims(r.Context(), claims)
			next.ServeHTTP(w, r.WithContext(newCtx))
		})
	}
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
		params.CustomClaimsConstructor = func(ctx context.Context) any {
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
				ID:                pulid.ID(r.Header.Get("Vc-Override-Id")),
				OrgID:             pulid.ID(r.Header.Get("Vc-Override-Org-Id")),
				AccountID:         "",
				OrgAccountID:      "",
				OrgMembershipRole: membershiprole.Admin,
				Role:              viewer.Employee,
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
