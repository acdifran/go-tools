package clerk

import (
	"context"
	"fmt"

	"github.com/acdifran/go-tools/common"
	"github.com/acdifran/go-tools/viewer"
)

func (c *ClerkClient) GetAccountInfoForViewer(
	ctx context.Context,
) (*common.AccountInfo, error) {
	vc := viewer.FromContext(ctx)

	var name, email string
	if !vc.HasOrg() {
		user, err := c.UserClient.Get(ctx, vc.AccountID)
		if err != nil {
			return nil, fmt.Errorf("getting clerk user: %w", err)
		}

		name = fmt.Sprintf("%v %v", user.FirstName, user.LastName)
		email = user.EmailAddresses[0].EmailAddress
	} else {
		org, err := c.OrganizationClient.Get(ctx, vc.OrgAccountID)
		if err != nil {
			return nil, fmt.Errorf("getting clerk organization: %w", err)
		}

		name = org.Name
	}

	return &common.AccountInfo{
		OrgID: vc.OrgID,
		Name:  name,
		Email: email,
	}, nil
}
