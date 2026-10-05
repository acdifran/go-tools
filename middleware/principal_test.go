package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/clerk/clerk-sdk-go/v2"

	"github.com/acdifran/go-tools/membershiprole"
	"github.com/acdifran/go-tools/viewer"
)

func TestViewerFromPrincipal(t *testing.T) {
	for _, tc := range []struct {
		name      string
		principal Principal
		want      viewer.Context
	}{
		{
			name: "org member on a paid plan",
			principal: Principal{
				ClerkUserID:  "user_1",
				ClerkOrgID:   "org_1",
				ClerkOrgRole: "org:admin",
				Claims: CustomClaims{
					UserID: "US1", Role: "USER", OrgID: "OG1", PersonalOrgID: "",
					Plan: "o:pro", PlanOverride: "", OrgPlanOverride: "",
				},
			},
			want: viewer.Context{
				Role: viewer.User, ID: "US1", OrgID: "OG1", AccountID: "user_1", OrgAccountID: "org_1",
				OrgMembershipRole: membershiprole.Admin, SubscriptionPlan: "pro",
			},
		},
		{
			name: "org plan override beats the billing plan",
			principal: Principal{
				ClerkUserID:  "user_1",
				ClerkOrgID:   "org_1",
				ClerkOrgRole: "org:member",
				Claims: CustomClaims{
					UserID: "US1", Role: "EMPLOYEE", OrgID: "OG1", PersonalOrgID: "",
					Plan: "o:pro", PlanOverride: "ignored", OrgPlanOverride: "enterprise",
				},
			},
			want: viewer.Context{
				Role: viewer.Employee, ID: "US1", OrgID: "OG1", AccountID: "user_1", OrgAccountID: "org_1",
				OrgMembershipRole: membershiprole.Member, SubscriptionPlan: "enterprise",
			},
		},
		{
			name: "no active org falls back to the personal org",
			principal: Principal{
				ClerkUserID:  "user_1",
				ClerkOrgID:   "",
				ClerkOrgRole: "",
				Claims: CustomClaims{
					UserID: "US1", Role: "USER", OrgID: "", PersonalOrgID: "OGP",
					Plan: "", PlanOverride: "", OrgPlanOverride: "",
				},
			},
			want: viewer.Context{
				Role: viewer.User, ID: "US1", OrgID: "OGP", AccountID: "user_1", OrgAccountID: "user_1",
				OrgMembershipRole: membershiprole.Admin, SubscriptionPlan: "free_user",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ViewerFromPrincipal(t.Context(), tc.principal, nil)
			if *got != tc.want {
				t.Errorf("viewer = %+v\nwant     %+v", *got, tc.want)
			}
		})
	}
}

func TestViewerFromPrincipalRejectsUnknownOrgRole(t *testing.T) {
	got := ViewerFromPrincipal(t.Context(), Principal{
		ClerkUserID:  "user_1",
		ClerkOrgID:   "org_1",
		ClerkOrgRole: "org:wizard",
		Claims: CustomClaims{
			UserID: "US1", Role: "USER", OrgID: "OG1", PersonalOrgID: "",
			Plan: "", PlanOverride: "", OrgPlanOverride: "",
		},
	}, nil)
	if !got.IsLoggedOut() {
		t.Errorf("viewer = %+v, want logged out", *got)
	}
}

func TestPrincipalFromClerkFollowsSessionTemplate(t *testing.T) {
	routes := map[string]any{
		"/v1/users/user_1": map[string]any{
			"object": "user", "id": "user_1",
			"public_metadata": map[string]any{
				"app_user_id": "US1", "app_user_role": "EMPLOYEE",
				"plan_override": "user_override", "app_personal_org_id": "OGP",
			},
		},
		"/v1/organizations/org_1": map[string]any{
			"object": "organization", "id": "org_1",
			"public_metadata": map[string]any{"app_org_id": "OG1", "plan_override": "org_override"},
		},
		"/v1/organizations/org_1/memberships": map[string]any{
			"data": []any{
				map[string]any{"object": "organization_membership", "role": "org:admin"},
			},
			"total_count": 1,
		},
		"/v1/billing/subscription_items": map[string]any{
			"data": []any{
				map[string]any{"status": "active", "plan": map[string]any{"slug": "pro"}},
			},
			"total_count": 1,
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/v1/billing/subscription_items" &&
			r.URL.Query().Get("organization_id") != "org_1" {
			t.Errorf(
				"subscription items queried with %q, want organization_id=org_1",
				r.URL.RawQuery,
			)
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	url := srv.URL + "/v1"
	clerk.SetBackend(clerk.NewBackend(&clerk.BackendConfig{
		HTTPClient: srv.Client(),
		URL:        &url,
		Key:        nil,
	}))

	got, err := PrincipalFromClerk(context.Background(), "user_1", "org_1")
	if err != nil {
		t.Fatal(err)
	}
	want := Principal{
		ClerkUserID:  "user_1",
		ClerkOrgID:   "org_1",
		ClerkOrgRole: "org:admin",
		Claims: CustomClaims{
			UserID: "US1", Role: "EMPLOYEE", OrgID: "OG1", PersonalOrgID: "OGP",
			Plan: "o:pro", PlanOverride: "user_override", OrgPlanOverride: "org_override",
		},
	}
	if got != want {
		t.Errorf("principal = %+v\nwant        %+v", got, want)
	}
}
