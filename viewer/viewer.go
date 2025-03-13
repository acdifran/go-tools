package viewer

import (
	"context"
	"fmt"

	"github.com/acdifran/go-tools/membershiprole"
	"github.com/acdifran/go-tools/pulid"
)

type ViewerRole string

const (
	LoggedOut   ViewerRole = "LoggedOut"
	User        ViewerRole = "User"
	Agent       ViewerRole = "Agent"
	AllPowerful ViewerRole = "AllPowerful"
	Omni        ViewerRole = "Omni"
	Test        ViewerRole = "Test"
	Employee    ViewerRole = "Employee"
)

type Viewer interface {
	GetBaseContext() *Context
}

type Context struct {
	Role              ViewerRole
	ID                pulid.ID
	OrgID             pulid.ID
	AccountID         string
	OrgAccountID      string
	OrgMembershipRole membershiprole.MembershipRole
}

func (c *Context) String() string {
	if c.IsAnonymous() {
		return fmt.Sprintf("VC(Role: %s)", c.Role)
	}
	return fmt.Sprintf(
		"VC(Role: %s, ID: %s, OrgID: %s)",
		c.Role,
		c.ID,
		c.OrgID,
	)
}

func (v *Context) IsAnonymous() bool {
	return v.ID == ""
}

func (v *Context) HasIdentity() bool {
	return !v.IsAnonymous()
}

func (v *Context) HasOrg() bool {
	return v.OrgID != ""
}

func (v *Context) IsAllPowerful() bool {
	return v.Role == AllPowerful
}

func (v *Context) IsOmni() bool {
	return v.Role == Omni
}

func (v *Context) IsLoggedOut() bool {
	return v.Role == LoggedOut
}

func (v *Context) IsTest() bool {
	return v.Role == Test
}

func (v *Context) IsOrgAdmin() bool {
	return v.OrgMembershipRole == membershiprole.Admin
}

func (v *Context) IsAgent() bool {
	return v.Role == Agent
}

func (v *Context) IsEmployee() bool {
	return v.Role == Employee
}

func AllPowerfulContext() *Context {
	return &Context{Role: AllPowerful}
}

func OmniContext() *Context {
	return &Context{Role: Omni}
}

func LoggedOutContext() *Context {
	return &Context{Role: LoggedOut}
}

func AllPowerfulVC(parent context.Context) context.Context {
	return NewContext(parent, AllPowerfulContext())
}

func OmniVC(parent context.Context) context.Context {
	return NewContext(parent, OmniContext())
}

func LoggedOutVC(parent context.Context) context.Context {
	return NewContext(parent, LoggedOutContext())
}

type CtxKey struct{}

func (c *Context) GetBaseContext() *Context {
	return c
}

func FromContext(ctx context.Context) *Context {
	v, _ := ctx.Value(CtxKey{}).(Viewer)
	return v.GetBaseContext()
}

func NewContext(parent context.Context, v *Context) context.Context {
	return context.WithValue(parent, CtxKey{}, v)
}
