package clerkhooks

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/acdifran/go-tools/logger"
	"github.com/acdifran/go-tools/membershiprole"
	"github.com/acdifran/go-tools/pulid"
	"github.com/samber/lo"

	"github.com/clerk/clerk-sdk-go/v2"
	clerkbilling "github.com/clerk/clerk-sdk-go/v2/billing"
	clerkorg "github.com/clerk/clerk-sdk-go/v2/organization"
	clerkuser "github.com/clerk/clerk-sdk-go/v2/user"
)

type App interface {
	logError(ctx context.Context, err error)
}

type User struct {
	ID            pulid.ID
	PersonalOrgID *pulid.ID
	ImageURL      *string
}

type Organization struct {
	ID pulid.ID
}

type UserInputData struct {
	FirstName    *string
	LastName     *string
	Username     *string
	ImageURL     *string
	EmailAddress *string
	Phone        *string
}

type OrgInputData struct {
	Name     string
	ImageURL *string
}

type CreateUserData struct {
	AccountID               string
	UserID                  *pulid.ID
	IsEmployee              bool
	PersonalOrgID           *pulid.ID
	ExternalAccountProvider *string
	Plan                    string
	UserInputData
}

type CreateOrgData struct {
	UserID       pulid.ID
	OrgID        *pulid.ID
	OrgAccountID string
	Plan         string
	OrgInputData
}

type CreateMembershipData struct {
	OrgID  pulid.ID
	UserID pulid.ID
	Role   membershiprole.MembershipRole
}

type AppClient interface {
	CreateMembership(ctx context.Context, data *CreateMembershipData) error
	CreateOrganization(ctx context.Context, data *CreateOrgData) (*Organization, error)
	CreateUser(ctx context.Context, data *CreateUserData) (*User, error)
	DeleteMembership(ctx context.Context, orgID pulid.ID, userID pulid.ID) error
	GetUser(ctx context.Context, userID pulid.ID) (*User, error)
	GetUserByAccountID(ctx context.Context, accountID string) (*User, error)
	GetOrgByAccountID(ctx context.Context, accountID string) (*Organization, error)
	GetUserByAccountIDOrNil(ctx context.Context, accountID string) (*User, error)
	GetOrgByAccountIDOrNil(ctx context.Context, accountID string) (*Organization, error)
	MembershipExists(ctx context.Context, orgID pulid.ID, userID pulid.ID) (bool, error)
	SetOrgDetails(ctx context.Context, orgID pulid.ID, data *OrgInputData) error
	SetUserProfileDetails(ctx context.Context, userID pulid.ID, data *UserInputData) error
	UpdateMembership(
		ctx context.Context,
		data *CreateMembershipData,
	) error
	SetUserSubscriptionPlan(
		ctx context.Context,
		userID pulid.ID,
		plan string,
	) error
	SetOrgSubscriptionPlan(
		ctx context.Context,
		orgID pulid.ID,
		plan string,
	) error
}

type webhookEvent struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type userData struct {
	ID               string  `json:"id"`
	ExternalID       string  `json:"external_id"`
	FirstName        *string `json:"first_name,omitempty"`
	LastName         *string `json:"last_name,omitempty"`
	Username         *string `json:"username,omitempty"`
	ImageURL         *string `json:"image_url,omitempty"`
	ExternalAccounts []struct {
		EmailAddress string `json:"email_address,omitempty"`
		Provider     string `json:"provider,omitempty"`
	} `json:"external_accounts,omitempty"`
	EmailAddresses []struct {
		EmailAddress string `json:"email_address"`
	} `json:"email_addresses"`
	PhoneNumbers []struct {
		PhoneNumber string `json:"phone_number"`
	} `json:"phone_numbers"`
}

type UserPublicMetadata struct {
	UserID        string `json:"app_user_id"`
	PersonalOrgID string `json:"app_personal_org_id"`
	Role          string `json:"app_user_role"`
}

type organizationData struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	ImageURL       *string           `json:"image_url,omitempty"`
	CreatedBy      string            `json:"created_by"`
	PublicMetadata orgPublicMetadata `json:"public_metadata"`
}

type orgPublicMetadata struct {
	OrgID string `json:"app_org_id"`
}

type membershipData struct {
	Organization struct {
		ID             string `json:"id"`
		PublicMetadata struct {
			OrgID string `json:"app_org_id"`
		} `json:"public_metadata"`
	} `json:"organization"`
	PublicUserData struct {
		UserID string `json:"user_id"`
	} `json:"public_user_data"`
	Role string `json:"role"`
}

func (c *ClerkHook) GetUserByAccountID(
	ctx context.Context,
	accountID string,
) (*User, error) {
	user, err := c.appClient.GetUserByAccountID(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("user with accountID %v not found: %w", accountID, err)
	}

	return user, nil
}

func (c *ClerkHook) isEmployeeEmail(email string) bool {
	return slices.Contains(c.employeeEmailConfig.emails, email) ||
		strings.HasSuffix(email, fmt.Sprintf("@%s", c.employeeEmailConfig.domain))
}

func (c *ClerkHook) handleUserCreated(
	ctx context.Context,
	data []byte,
) error {
	var userData userData
	if err := json.Unmarshal(data, &userData); err != nil {
		return fmt.Errorf("reading UserData: %w", err)
	}

	existingUser, err := c.appClient.GetUserByAccountIDOrNil(ctx, userData.ID)
	if err != nil {
		return fmt.Errorf("checking for existing user with accountID %s: %w", userData.ID, err)
	}
	if existingUser != nil {
		return nil
	}

	isEmployee := false
	for _, email := range userData.EmailAddresses {
		if c.isEmployeeEmail(email.EmailAddress) {
			isEmployee = true
			break
		}
	}

	emailAddress := lo.EmptyableToPtr(lo.FirstOrEmpty(userData.EmailAddresses).EmailAddress)
	phone := lo.EmptyableToPtr(lo.FirstOrEmpty(userData.PhoneNumbers).PhoneNumber)
	externalAccountProvider := lo.EmptyableToPtr(
		lo.FirstOrEmpty(userData.ExternalAccounts).Provider,
	)

	plans, err := clerkbilling.ListSubscriptionItems(
		ctx,
		&clerkbilling.ListSubscriptionItemsParams{UserID: &userData.ID, Status: lo.ToPtr("active")},
	)
	if err != nil {
		return fmt.Errorf("listing subscription items: %w", err)
	}

	plan := "free_user"
	if len(plans.Data) > 0 {
		plan = plans.Data[0].Plan.Slug
	}

	user, err := c.appClient.CreateUser(ctx, &CreateUserData{
		AccountID:  userData.ID,
		IsEmployee: isEmployee,
		UserInputData: UserInputData{
			FirstName:    userData.FirstName,
			LastName:     userData.LastName,
			Username:     userData.Username,
			ImageURL:     userData.ImageURL,
			EmailAddress: emailAddress,
			Phone:        phone,
		},
		PersonalOrgID:           nil,
		UserID:                  nil,
		ExternalAccountProvider: externalAccountProvider,
		Plan:                    plan,
	})
	if err != nil {
		return err
	}

	return UpdateCreatedUser(ctx, user.ID, userData.ID, isEmployee, user.PersonalOrgID)
}

func UpdateCreatedUser(
	ctx context.Context,
	userID pulid.ID,
	AccountID string,
	isEmployee bool,
	personalOrgID *pulid.ID,
) error {
	role := lo.Ternary(isEmployee, "EMPLOYEE", "USER")
	userIDstr := string(userID)

	publicMetadata := &UserPublicMetadata{UserID: userIDstr, Role: role, PersonalOrgID: ""}
	if personalOrgID != nil {
		publicMetadata.PersonalOrgID = string(*personalOrgID)
	}

	publicMetadataJSON, err := json.Marshal(publicMetadata)
	if err != nil {
		return fmt.Errorf("writing UserPublicMetadata: %w", err)
	}

	rawMessage := json.RawMessage(publicMetadataJSON)
	_, err = clerkuser.Update(ctx, AccountID, &clerkuser.UpdateParams{
		ExternalID:     &userIDstr,
		PublicMetadata: &rawMessage,
	})
	if err != nil {
		return fmt.Errorf("updating clerk user: %w", err)
	}
	return nil
}

func (c *ClerkHook) handleUserUpdated(ctx context.Context, data []byte) error {
	var userData userData
	if err := json.Unmarshal(data, &userData); err != nil {
		return fmt.Errorf("reading UserData: %w", err)
	}

	user, err := c.appClient.GetUser(ctx, pulid.ID(userData.ExternalID))
	if err != nil {
		return fmt.Errorf("getting User to update: %w", err)
	}

	emailAddress := lo.EmptyableToPtr(lo.FirstOrEmpty(userData.EmailAddresses).EmailAddress)
	phone := lo.EmptyableToPtr(lo.FirstOrEmpty(userData.PhoneNumbers).PhoneNumber)

	err = c.appClient.SetUserProfileDetails(ctx, user.ID, &UserInputData{
		FirstName:    userData.FirstName,
		LastName:     userData.LastName,
		Username:     userData.Username,
		ImageURL:     userData.ImageURL,
		EmailAddress: emailAddress,
		Phone:        phone,
	})
	if err != nil {
		return err
	}

	return nil
}

func (c *ClerkHook) GetOrCreateUserByAccountID(
	ctx context.Context,
	accountID string,
	userID *pulid.ID,
	personalOrgID *pulid.ID,
	isEmployee bool,
) (*User, error) {
	user, err := c.appClient.GetUserByAccountIDOrNil(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf(
			"fetching user with accountID: %s: %w",
			accountID,
			err,
		)
	}

	if user != nil {
		return user, nil
	}

	clerkUser, err := clerkuser.Get(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf(
			"fetching clerk user with id: %s: %w",
			accountID,
			err,
		)
	}

	emailAddress := lo.EmptyableToPtr(lo.FirstOrEmpty(clerkUser.EmailAddresses).EmailAddress)
	phoneNumber := lo.FirstOrEmpty(clerkUser.PhoneNumbers)
	phone := lo.EmptyableToPtr("")
	if phoneNumber != nil {
		lo.EmptyableToPtr(phoneNumber.PhoneNumber)
	}
	provider := lo.EmptyableToPtr(lo.FirstOrEmpty(clerkUser.ExternalAccounts).Provider)
	logger.Debug("creating account", "email", emailAddress, "provider", provider)

	createdUser, err := c.appClient.CreateUser(ctx, &CreateUserData{
		AccountID: accountID,
		UserID:    userID,
		UserInputData: UserInputData{
			FirstName:    clerkUser.FirstName,
			LastName:     clerkUser.LastName,
			Username:     clerkUser.Username,
			ImageURL:     clerkUser.ImageURL,
			EmailAddress: emailAddress,
			Phone:        phone,
		},
		PersonalOrgID:           personalOrgID,
		IsEmployee:              isEmployee,
		ExternalAccountProvider: provider,
		Plan:                    "free_user",
	})
	if err != nil {
		return nil, fmt.Errorf("creating user: %w", err)
	}

	publicMetadata := &UserPublicMetadata{}
	err = json.Unmarshal(clerkUser.PublicMetadata, publicMetadata)
	if err != nil && publicMetadata.UserID == "" {
		createdUserIDStr := string(createdUser.ID)
		personalOrgIDstr := ""
		if personalOrgID != nil {
			personalOrgIDstr = string(*personalOrgID)
		}
		publicMetadata := &UserPublicMetadata{
			UserID:        createdUserIDStr,
			Role:          "EMPLOYEE",
			PersonalOrgID: personalOrgIDstr,
		}

		publicMetadataJSON, err := json.Marshal(publicMetadata)
		if err != nil {
			slog.Error("error writing UserPublicMetadata", "err", err)
		}

		rawMessage := json.RawMessage(publicMetadataJSON)
		_, err = clerkuser.Update(ctx, accountID, &clerkuser.UpdateParams{
			ExternalID:     &createdUserIDStr,
			PublicMetadata: &rawMessage,
		})
		if err != nil {
			slog.Error("error updating clerk user", "err", err)
		}
	}

	return createdUser, nil
}

func (c *ClerkHook) GetOrCreateOrgByAccountID(
	ctx context.Context,
	accountID string,
	userID pulid.ID,
	orgID *pulid.ID,
) (*Organization, error) {
	org, err := c.appClient.GetOrgByAccountIDOrNil(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf(
			"fetching org with accountID: %s: %w",
			accountID,
			err,
		)
	}

	if org != nil {
		return org, nil
	}

	clerkOrg, err := clerkorg.Get(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf(
			"fetching clerk org with id: %s: %w",
			accountID,
			err,
		)
	}

	createdOrg, err := c.appClient.CreateOrganization(ctx, &CreateOrgData{
		OrgAccountID: accountID,
		UserID:       userID,
		OrgID:        orgID,
		OrgInputData: OrgInputData{
			Name:     clerkOrg.Name,
			ImageURL: clerkOrg.ImageURL,
		},
		Plan: "free_org",
	})
	if err != nil {
		return nil, fmt.Errorf("creating org: %w", err)
	}

	publicMetadata := &orgPublicMetadata{}
	err = json.Unmarshal(clerkOrg.PublicMetadata, publicMetadata)
	if err != nil && publicMetadata.OrgID == "" {
		createdOrgIDStr := string(createdOrg.ID)
		publicMetadata := &orgPublicMetadata{
			OrgID: createdOrgIDStr,
		}

		publicMetadataJSON, err := json.Marshal(publicMetadata)
		if err != nil {
			slog.Error("error writing OrgPublicMetadata", "err", err)
		}

		rawMessage := json.RawMessage(publicMetadataJSON)
		_, err = clerkorg.Update(ctx, accountID, &clerkorg.UpdateParams{
			PublicMetadata: &rawMessage,
		})
		if err != nil {
			slog.Error("error updating clerk org", "err", err)
		}
	}

	return createdOrg, nil
}

func (c *ClerkHook) GetPersonalOrgByAccountID(
	ctx context.Context,
	accountID string,
	user *User,
	orgID *pulid.ID,
) (*Organization, error) {
	return c.appClient.GetOrgByAccountID(ctx, accountID)
}

func (c *ClerkHook) handleOrganizationCreated(ctx context.Context, data []byte) error {
	var orgData organizationData
	if err := json.Unmarshal(data, &orgData); err != nil {
		return fmt.Errorf("reading OrganizationData: %w", err)
	}

	accountID := orgData.CreatedBy
	user, err := c.appClient.GetUserByAccountID(ctx, accountID)
	if err != nil {
		slog.Error("creating org", "error", err)
	}

	org, err := c.appClient.CreateOrganization(ctx, &CreateOrgData{
		UserID:       user.ID,
		OrgAccountID: orgData.ID,
		OrgInputData: OrgInputData{
			Name:     orgData.Name,
			ImageURL: orgData.ImageURL,
		},
		OrgID: nil,
		Plan:  "free_org",
	})
	if err != nil {
		return err
	}

	publicMetadata := &orgPublicMetadata{OrgID: string(org.ID)}
	publicMetadataJSON, err := json.Marshal(publicMetadata)
	if err != nil {
		return fmt.Errorf("writing OrgPublicMetadata: %w", err)
	}

	rawMessage := json.RawMessage(publicMetadataJSON)
	_, err = clerkorg.Update(ctx, orgData.ID, &clerkorg.UpdateParams{
		PublicMetadata: &rawMessage,
	})
	if err != nil {
		return fmt.Errorf("updating clerk org: %w", err)
	}

	return nil
}

func (c *ClerkHook) handleOrganizationUpdated(ctx context.Context, data []byte) error {
	var orgData organizationData
	if err := json.Unmarshal(data, &orgData); err != nil {
		return fmt.Errorf("reading OrganizationData: %w", err)
	}

	err := c.appClient.SetOrgDetails(ctx, pulid.ID(orgData.PublicMetadata.OrgID), &OrgInputData{
		Name:     orgData.Name,
		ImageURL: orgData.ImageURL,
	})
	if err != nil {
		return err
	}

	return nil
}

func (c *ClerkHook) handleOrganizationMembershipCreated(
	ctx context.Context,
	data []byte,
) error {
	var membershipData membershipData
	if err := json.Unmarshal(data, &membershipData); err != nil {
		return err
	}

	userAccountID := membershipData.PublicUserData.UserID
	user, err := c.appClient.GetUserByAccountID(ctx, userAccountID)
	if err != nil {
		return fmt.Errorf("getting user by ID: %w", err)
	}

	orgAccountID := membershipData.Organization.ID
	org, err := c.appClient.GetOrgByAccountID(ctx, orgAccountID)
	if err != nil {
		return fmt.Errorf("getting org by ID: %w", err)
	}

	// Check if membership already exists to avoid erroring in webhooks.
	// When a new organization is created, it fires the organization.Created event and the
	// organizationMembership.Created event, so the second hook fails when writing the
	// membership due to it already existing from the org Create Action call from the first hook.
	exists, err := c.appClient.MembershipExists(ctx, org.ID, user.ID)
	if err == nil && exists {
		return nil
	}

	role := membershiprole.Member
	if membershipData.Role == "org:admin" {
		role = membershiprole.Admin
	}

	err = c.appClient.CreateMembership(ctx, &CreateMembershipData{
		OrgID:  org.ID,
		UserID: user.ID,
		Role:   role,
	})
	if err != nil {
		return fmt.Errorf("setting user %s as %s for org %s: %w", user.ID, role, org.ID, err)
	}

	return nil
}

func (c *ClerkHook) handleOrganizationMembershipUpdated(
	ctx context.Context,
	data []byte,
) error {
	var membershipData membershipData
	if err := json.Unmarshal(data, &membershipData); err != nil {
		return err
	}

	user, err := c.appClient.GetUserByAccountID(ctx, membershipData.PublicUserData.UserID)
	if err != nil {
		return err
	}

	orgID := pulid.ID(membershipData.Organization.PublicMetadata.OrgID)

	role := membershiprole.Member
	if membershipData.Role == "org:admin" {
		role = membershiprole.Admin
	}

	err = c.appClient.UpdateMembership(
		ctx,
		&CreateMembershipData{UserID: user.ID, OrgID: orgID, Role: role},
	)
	if err != nil {
		return fmt.Errorf("setting user %s as %s for org %s: %w", user.ID, role, orgID, err)
	}

	return nil
}

func (c *ClerkHook) handleOrganizationMembershipDeleted(
	ctx context.Context,
	data []byte,
) error {
	var membershipData membershipData
	if err := json.Unmarshal(data, &membershipData); err != nil {
		return err
	}

	user, err := c.appClient.GetUserByAccountID(ctx, membershipData.PublicUserData.UserID)
	if err != nil {
		return err
	}

	orgID := pulid.ID(membershipData.Organization.PublicMetadata.OrgID)
	role, err := membershiprole.Coerce(membershipData.Role)
	if err != nil {
		return err
	}

	err = c.appClient.DeleteMembership(ctx, orgID, user.ID)
	if err != nil {
		return fmt.Errorf("deleting user %s as %s for org %s: %w", user.ID, role, orgID, err)
	}

	return nil
}

func (c *ClerkHook) handleSubscriptionItemActive(
	ctx context.Context,
	data []byte,
) error {
	var subscriptionItem clerk.SubscriptionItem
	if err := json.Unmarshal(data, &subscriptionItem); err != nil {
		return err
	}

	userID := subscriptionItem.Payer.UserID
	if userID != nil && *userID != "" {
		user, err := c.appClient.GetUserByAccountIDOrNil(ctx, *userID)
		if err != nil {
			return fmt.Errorf("getting user with ID %s: %w", *userID, err)
		}

		if user == nil {
			logger.WarnContext(
				ctx,
				"User not found when handling subscription item active",
				"userID",
				*userID,
			)
			return nil
		}

		err = c.appClient.SetUserSubscriptionPlan(ctx, user.ID, subscriptionItem.Plan.Slug)
		if err != nil {
			return fmt.Errorf(
				"setting user %s subscription plan to %s: %w",
				user.ID,
				subscriptionItem.Plan.Slug,
				err,
			)
		}
	}

	orgID := subscriptionItem.Payer.OrganizationID
	if orgID != nil && *orgID != "" {
		org, err := c.appClient.GetOrgByAccountID(ctx, *orgID)
		if err != nil {
			return fmt.Errorf("getting org with ID %s: %w", *orgID, err)
		}

		err = c.appClient.SetOrgSubscriptionPlan(ctx, org.ID, subscriptionItem.Plan.Slug)
		if err != nil {
			return fmt.Errorf(
				"setting org %s subscription plan to %s: %w",
				org.ID,
				subscriptionItem.Plan.Slug,
				err,
			)
		}
	}

	return nil
}

func (c *ClerkHook) HandleHooks(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
) error {
	ctx = logger.AppendCtx(ctx, slog.String("webhook", "clerk"))

	payload, err := io.ReadAll(r.Body)
	if err != nil {
		return fmt.Errorf("failed to read request: %w", err)
	}
	defer r.Body.Close()

	err = c.wh.Verify(payload, r.Header)
	if err != nil {
		return fmt.Errorf("invalid webhook signature: %w", err)
	}

	var event webhookEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return fmt.Errorf("failed to parse webhook payload: %w", err)
	}

	switch event.Type {
	case "user.created":
		err = c.handleUserCreated(ctx, event.Data)
	case "user.updated":
		err = c.handleUserUpdated(ctx, event.Data)
	case "organization.created":
		err = c.handleOrganizationCreated(ctx, event.Data)
	case "organization.updated":
		err = c.handleOrganizationUpdated(ctx, event.Data)
	case "organizationMembership.created":
		err = c.handleOrganizationMembershipCreated(ctx, event.Data)
	case "organizationMembership.updated":
		err = c.handleOrganizationMembershipUpdated(ctx, event.Data)
	case "organizationMembership.deleted":
		err = c.handleOrganizationMembershipDeleted(ctx, event.Data)
	case "subscriptionItem.active":
		err = c.handleSubscriptionItemActive(ctx, event.Data)
	default:
		return fmt.Errorf("unhandled event type")
	}

	if err != nil {
		err = fmt.Errorf("handling %s event: %w", event.Type, err)
	}

	return err
}
