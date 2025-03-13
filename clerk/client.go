package clerk

import (
	"fmt"

	"github.com/acdifran/go-tools/membershiprole"
	"github.com/clerk/clerk-sdk-go/v2"
	clerkorg "github.com/clerk/clerk-sdk-go/v2/organization"
	clerkuser "github.com/clerk/clerk-sdk-go/v2/user"
)

type ClerkClient struct {
	UserClient         *clerkuser.Client
	OrganizationClient *clerkorg.Client
}

func NewClerkClient(key string) *ClerkClient {
	config := &clerk.ClientConfig{}
	config.Key = &key
	userClient := clerkuser.NewClient(config)
	organizationClient := clerkorg.NewClient(config)
	return &ClerkClient{
		UserClient:         userClient,
		OrganizationClient: organizationClient,
	}
}

func ClerkRoleToMembershipRole(val string) (membershiprole.MembershipRole, error) {
	switch val {
	case "org:admin":
		return membershiprole.Admin, nil
	case "org:member":
		return membershiprole.Member, nil
	default:
		return "", fmt.Errorf("%s is not a valid MembershipRole", val)
	}
}
