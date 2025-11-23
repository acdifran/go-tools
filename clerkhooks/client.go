package clerkhooks

import svix "github.com/svix/svix-webhooks/go"

type employeeEmailConfig struct {
	emails []string
	domain string
}

type ClerkHook struct {
	appClient               AppClient
	wh                      *svix.Webhook
	employeeEmailConfig     employeeEmailConfig
	shouldCreatePersonalOrg bool
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
