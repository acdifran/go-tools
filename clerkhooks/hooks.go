package clerkhooks

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/acdifran/go-tools/logger"
	"github.com/acdifran/go-tools/membershiprole"
	"github.com/acdifran/go-tools/pulid"

	clerkorg "github.com/clerk/clerk-sdk-go/v2/organization"
	clerkuser "github.com/clerk/clerk-sdk-go/v2/user"
)

type App interface {
	logError(ctx context.Context, err error)
}

type User struct {
	ID            pulid.ID
	PersonalOrgID *pulid.ID
}

type Organization struct {
	ID pulid.ID
}

type UserInputData struct {
	FirstName    string
	LastName     string
	Username     *string
	ImageURL     string
	EmailAddress *string
	Phone        *string
}

type OrgInputData struct {
	Name     string
	ImageURL string
}

type CreateUserData struct {
	AccountID               string
	IsEmployee              bool
	ShouldCreatePersonalOrg bool
	UserInputData
}

type CreateOrgData struct {
	UserID       pulid.ID
	OrgAccountID string
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
	MembershipExists(ctx context.Context, orgID pulid.ID, userID pulid.ID) (bool, error)
	SetOrgDetails(ctx context.Context, orgID pulid.ID, data *OrgInputData) error
	SetUserProfileDetails(ctx context.Context, userID pulid.ID, data *UserInputData) error
	UpdateMembership(
		ctx context.Context,
		data *CreateMembershipData,
	) error
}

type webhookEvent struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type userData struct {
	ID             string  `json:"id"`
	ExternalID     string  `json:"external_id"`
	FirstName      string  `json:"first_name"`
	LastName       string  `json:"last_name"`
	Username       *string `json:"username"`
	ImageURL       string  `json:"image_url"`
	EmailAddresses []struct {
		EmailAddress string `json:"email_address"`
	} `json:"email_addresses"`
	PhoneNumbers []struct {
		PhoneNumber string `json:"phone_number"`
	} `json:"phone_numbers"`
}

type userPublicMetadata struct {
	UserID        string `json:"app_user_id"`
	PersonalOrgID string `json:"app_personal_org_id"`
	Role          string `json:"app_user_role"`
}

type organizationData struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	ImageURL       string            `json:"image_url"`
	CreatedBy      string            `json:"created_by"`
	PublicMetadata orgPublicMetadata `json:"public_metadata"`
}

type orgPublicMetadata struct {
	OrgID string `json:"app_org_id"`
}

type membershipData struct {
	Organization struct {
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

func (c *ClerkHook) handleUserCreated(
	ctx context.Context,
	data []byte,
	shouldCreatePersonalOrg bool,
) error {
	var userData userData
	if err := json.Unmarshal(data, &userData); err != nil {
		return fmt.Errorf("reading UserData: %w", err)
	}

	isEmployee := false
	for _, email := range userData.EmailAddresses {
		// if strings.HasSuffix(email.EmailAddress, "@mydomain.com") {
		if email.EmailAddress == "acdifran@gmail.com" {
			isEmployee = true
			break
		}
	}

	var emailAddress, phone *string

	if len(userData.EmailAddresses) > 0 {
		emailAddress = &userData.EmailAddresses[0].EmailAddress
	}
	if len(userData.PhoneNumbers) > 0 {
		phone = &userData.PhoneNumbers[0].PhoneNumber
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
	})
	if err != nil {
		return err
	}

	userID := string(user.ID)
	role := "USER"
	if isEmployee {
		role = "EMPLOYEE"
	}

	publicMetadata := &userPublicMetadata{UserID: userID, Role: role}
	if shouldCreatePersonalOrg {
		if user.PersonalOrgID == nil {
			return fmt.Errorf("user %s does not have a personal org", user.ID)
		}
		publicMetadata.PersonalOrgID = string(*user.PersonalOrgID)
	}

	publicMetadataJSON, err := json.Marshal(publicMetadata)
	if err != nil {
		return fmt.Errorf("writing UserPublicMetadata: %w", err)
	}

	rawMessage := json.RawMessage(publicMetadataJSON)
	_, err = clerkuser.Update(ctx, userData.ID, &clerkuser.UpdateParams{
		ExternalID:     &userID,
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

	var emailAddress, phone *string

	if len(userData.EmailAddresses) > 0 {
		emailAddress = &userData.EmailAddresses[0].EmailAddress
	}
	if len(userData.PhoneNumbers) > 0 {
		phone = &userData.PhoneNumbers[0].PhoneNumber
	}

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

func (c *ClerkHook) handleOrganizationCreated(ctx context.Context, data []byte) error {
	var orgData organizationData
	if err := json.Unmarshal(data, &orgData); err != nil {
		return fmt.Errorf("reading OrganizationData: %w", err)
	}

	accountID := orgData.CreatedBy
	user, err := c.appClient.GetUserByAccountID(ctx, accountID)
	if err != nil {
		return fmt.Errorf("user (accountID: %s) that created org not found: %w", accountID, err)
	}

	org, err := c.appClient.CreateOrganization(ctx, &CreateOrgData{
		UserID:       user.ID,
		OrgAccountID: orgData.ID,
		OrgInputData: OrgInputData{
			Name:     orgData.Name,
			ImageURL: orgData.ImageURL,
		},
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

	err := c.appClient.SetOrgDetails(ctx, pulid.ID(orgData.CreatedBy), &OrgInputData{
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

	accountID := membershipData.PublicUserData.UserID
	user, err := c.appClient.GetUserByAccountID(ctx, accountID)
	if err != nil {
		return fmt.Errorf("user with accountID: %s not found: %w", accountID, err)
	}

	orgID := pulid.ID(membershipData.Organization.PublicMetadata.OrgID)

	// Check if membership already exists to avoid erroring in webhooks.
	// When a new organization is created, it fires the organization.Created event and the
	// organizationMembership.Created event, so the second hook fails when writing the
	// membership due to it already existing from the org CreateWithAdmin call from the first hook.
	exists, err := c.appClient.MembershipExists(ctx, orgID, user.ID)
	if err == nil && exists {
		return nil
	}

	role := membershiprole.Member
	if membershipData.Role == "org:admin" {
		role = membershiprole.Admin
	}

	err = c.appClient.CreateMembership(ctx, &CreateMembershipData{
		OrgID:  orgID,
		UserID: user.ID,
		Role:   role,
	})
	if err != nil {
		return fmt.Errorf("setting user %s as %s for org %s: %w", user.ID, role, orgID, err)
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

type clerkHookOptions struct {
	shouldCreatePersonalOrg bool
}

type ClerkHookOption func(*clerkHookOptions)

func WithPersonalOrgs(shouldCreatePersonalOrg bool) ClerkHookOption {
	return func(opts *clerkHookOptions) {
		opts.shouldCreatePersonalOrg = shouldCreatePersonalOrg
	}
}

func (c *ClerkHook) HandleHooks(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	secretKey string,
	opts ...ClerkHookOption,
) error {
	ctx = logger.AppendCtx(ctx, slog.String("webhook", "clerk"))

	clerkOpts := &clerkHookOptions{}
	for _, opt := range opts {
		opt(clerkOpts)
	}

	payload, err := io.ReadAll(r.Body)
	if err != nil {
		return fmt.Errorf("Failed to read request: %w", err)
	}
	defer r.Body.Close()

	err = c.wh.Verify(payload, r.Header)
	if err != nil {
		return fmt.Errorf("Invalid webhook signature: %w", err)
	}

	var event webhookEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return fmt.Errorf("Failed to parse webhook payload: %w", err)
	}

	switch event.Type {
	case "user.created":
		err = c.handleUserCreated(ctx, event.Data, clerkOpts.shouldCreatePersonalOrg)
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
	default:
		return fmt.Errorf("Unhandled event type")
	}

	if err != nil {
		ctx = logger.AppendCtx(ctx, slog.String("clerk_event", event.Type))
	}

	return err
}
