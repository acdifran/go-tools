package clerkhooks

import (
	"context"

	svix "github.com/svix/svix-webhooks/go"
)

type employeeEmailConfig struct {
	emails []string
	domain string
}

type ClerkHook struct {
	appClient               AppClient
	wh                      *svix.Webhook
	employeeEmailConfig     employeeEmailConfig
	shouldCreatePersonalOrg bool
	onAuthDataChanged       func(context.Context)
}

type ClerkHookOption func(*ClerkHook)

func NewClerkWebhook(
	appClient AppClient,
	wh *svix.Webhook,
	opts ...ClerkHookOption,
) *ClerkHook {
	clerkHook := &ClerkHook{
		appClient: appClient,
		wh:        wh,
		employeeEmailConfig: employeeEmailConfig{
			emails: []string{},
			domain: "",
		},
		shouldCreatePersonalOrg: false,
	}

	for _, opt := range opts {
		opt(clerkHook)
	}

	return clerkHook
}

// Lets callers drop cached auth data (see middleware.PrincipalCache) when Clerk reports a change.
func WithOnAuthDataChanged(fn func(context.Context)) ClerkHookOption {
	return func(opts *ClerkHook) {
		opts.onAuthDataChanged = fn
	}
}

func WithPersonalOrgs() ClerkHookOption {
	return func(opts *ClerkHook) {
		opts.shouldCreatePersonalOrg = true
	}
}

func WithEmployeeEmails(emails []string) ClerkHookOption {
	return func(opts *ClerkHook) {
		opts.employeeEmailConfig.emails = emails
	}
}

func WithEmployeeEmailDomain(domain string) ClerkHookOption {
	return func(opts *ClerkHook) {
		opts.employeeEmailConfig.domain = domain
	}
}
