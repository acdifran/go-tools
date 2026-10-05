package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	memoize "github.com/agkloop/go_memoize"
	"github.com/agkloop/go_memoize/stores/memory"
	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/clerk/clerk-sdk-go/v2/billing"
	"github.com/clerk/clerk-sdk-go/v2/organization"
	"github.com/clerk/clerk-sdk-go/v2/organizationmembership"
	"github.com/clerk/clerk-sdk-go/v2/user"
	"github.com/samber/lo"

	clerktools "github.com/acdifran/go-tools/clerk"
	"github.com/acdifran/go-tools/clerkhooks"
	"github.com/acdifran/go-tools/membershiprole"
	"github.com/acdifran/go-tools/pulid"
	"github.com/acdifran/go-tools/viewer"
)

var NotOrgMemberError = errors.New("user is not a member of the organization")

// Principal is what a viewer is built from, so session tokens and OAuth tokens produce the same viewer.
type Principal struct {
	ClerkUserID  string
	ClerkOrgID   string
	ClerkOrgRole string
	Claims       CustomClaims
}

func ViewerFromPrincipal(
	ctx context.Context,
	p Principal,
	clerkHook *clerkhooks.ClerkHook,
) *viewer.Context {
	claims := p.Claims
	if claims.UserID == "" {
		slog.Info("missing user ID in claims, creating user", "subject", p.ClerkUserID)
		userData, err := clerkHook.CreateNewUserFromClerkUser(
			viewer.AllPowerfulContext(ctx),
			p.ClerkUserID,
		)
		if err != nil {
			slog.Error("creating user", "error", err)
			return viewer.LoggedOutVC()
		}
		claims.UserID = string(userData.UserID)
		claims.PersonalOrgID = string(lo.FromPtr(userData.PersonalOrgID))
		claims.Role = userData.Role
	}

	orgID := claims.OrgID
	var orgAccountID string
	var orgMembershipRole membershiprole.MembershipRole
	if orgID != "" {
		orgAccountID = p.ClerkOrgID
		role, err := clerktools.ClerkRoleToMembershipRole(p.ClerkOrgRole)
		if err != nil {
			slog.Error("invalid organization role", "role", p.ClerkOrgRole, "error", err)
			return viewer.LoggedOutVC()
		}
		orgMembershipRole = role
	}

	plan := lo.Ternary(p.ClerkOrgID == "", "free_user", "free_org")
	if p.ClerkOrgID != "" && claims.OrgPlanOverride != "" {
		plan = claims.OrgPlanOverride
	} else if p.ClerkOrgID == "" && claims.PlanOverride != "" {
		plan = claims.PlanOverride
	} else if parts := strings.Split(claims.Plan, ":"); len(parts) > 1 {
		plan = parts[1]
	}

	if orgID == "" && claims.PersonalOrgID != "" {
		orgID = claims.PersonalOrgID
		orgAccountID = p.ClerkUserID
		orgMembershipRole = membershiprole.Admin
	}

	return &viewer.Context{
		Role:              lo.Ternary(claims.Role == "EMPLOYEE", viewer.Employee, viewer.User),
		ID:                pulid.ID(claims.UserID),
		OrgID:             pulid.ID(orgID),
		AccountID:         p.ClerkUserID,
		OrgAccountID:      orgAccountID,
		OrgMembershipRole: orgMembershipRole,
		SubscriptionPlan:  plan,
	}
}

// Mirrors the session-token template for Clerk OAuth tokens, which carry only user and org IDs.
func PrincipalFromClerk(ctx context.Context, clerkUserID, clerkOrgID string) (Principal, error) {
	u, err := user.Get(ctx, clerkUserID)
	if err != nil {
		return Principal{}, fmt.Errorf("getting clerk user: %w", err)
	}
	var userMeta struct {
		AppUserID        string `json:"app_user_id"`
		AppUserRole      string `json:"app_user_role"`
		PlanOverride     string `json:"plan_override"`
		AppPersonalOrgID string `json:"app_personal_org_id"`
	}
	if err := unmarshalMetadata(u.PublicMetadata, &userMeta); err != nil {
		return Principal{}, fmt.Errorf("reading user public metadata: %w", err)
	}

	p := Principal{
		ClerkUserID:  clerkUserID,
		ClerkOrgID:   clerkOrgID,
		ClerkOrgRole: "",
		Claims: CustomClaims{
			UserID:          userMeta.AppUserID,
			Role:            userMeta.AppUserRole,
			OrgID:           "",
			PersonalOrgID:   userMeta.AppPersonalOrgID,
			Plan:            "",
			PlanOverride:    userMeta.PlanOverride,
			OrgPlanOverride: "",
		},
	}

	planParams := &billing.ListSubscriptionItemsParams{
		Status:      lo.ToPtr("active"),
		IncludeFree: lo.ToPtr(true),
		UserID:      lo.ToPtr(clerkUserID),
	}
	planPrefix := "u:"
	if clerkOrgID != "" {
		if err := addOrgClaims(ctx, &p); err != nil {
			return Principal{}, err
		}
		planParams.UserID = nil
		planParams.OrganizationID = lo.ToPtr(clerkOrgID)
		planPrefix = "o:"
	}

	items, err := billing.ListSubscriptionItems(ctx, planParams)
	if err != nil {
		return Principal{}, fmt.Errorf("listing clerk subscription items: %w", err)
	}
	if item, ok := lo.Find(
		items.Data,
		func(i clerk.SubscriptionItem) bool { return i.Plan != nil },
	); ok {
		p.Claims.Plan = planPrefix + item.Plan.Slug
	}
	return p, nil
}

func addOrgClaims(ctx context.Context, p *Principal) error {
	org, err := organization.Get(ctx, p.ClerkOrgID)
	if err != nil {
		return fmt.Errorf("getting clerk organization: %w", err)
	}
	var orgMeta struct {
		AppOrgID     string `json:"app_org_id"`
		PlanOverride string `json:"plan_override"`
	}
	if err := unmarshalMetadata(org.PublicMetadata, &orgMeta); err != nil {
		return fmt.Errorf("reading organization public metadata: %w", err)
	}
	p.Claims.OrgID = orgMeta.AppOrgID
	p.Claims.OrgPlanOverride = orgMeta.PlanOverride

	memberships, err := organizationmembership.List(ctx, &organizationmembership.ListParams{
		OrganizationID: p.ClerkOrgID,
		UserIDs:        []string{p.ClerkUserID},
	})
	if err != nil {
		return fmt.Errorf("listing clerk organization memberships: %w", err)
	}
	if len(memberships.OrganizationMemberships) == 0 {
		return fmt.Errorf(
			"%w: user %s, organization %s",
			NotOrgMemberError,
			p.ClerkUserID,
			p.ClerkOrgID,
		)
	}
	p.ClerkOrgRole = memberships.OrganizationMemberships[0].Role
	return nil
}

func unmarshalMetadata(raw json.RawMessage, into any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, into)
}

type principalKey struct {
	clerkUserID string
	clerkOrgID  string
}

type PrincipalCache struct {
	cache *memoize.Cache[principalKey, Principal]
}

// Per-instance cache: ttl bounds staleness when a webhook clears a different instance.
func NewPrincipalCache(ttl time.Duration, capacity int) (*PrincipalCache, error) {
	cache, err := memoize.New[principalKey, Principal](
		memoize.Opts().
			WithStore(memory.New[principalKey, Principal](capacity)).
			WithTTL(ttl).
			WithTickerClock(time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("building principal cache: %w", err)
	}
	return &PrincipalCache{cache: cache}, nil
}

func (c *PrincipalCache) Get(
	ctx context.Context,
	clerkUserID, clerkOrgID string,
) (Principal, error) {
	return c.cache.GetOrCompute(
		ctx,
		principalKey{clerkUserID: clerkUserID, clerkOrgID: clerkOrgID},
		func(ctx context.Context) (Principal, error) {
			return PrincipalFromClerk(ctx, clerkUserID, clerkOrgID)
		},
	)
}

func (c *PrincipalCache) Clear(ctx context.Context) error {
	return c.cache.Clear(ctx)
}
