package clerkhooks

import svix "github.com/svix/svix-webhooks/go"

type ClerkHook struct {
	appClient AppClient
	wh        *svix.Webhook
}

func NewClerkWebhook(appClient AppClient, wh *svix.Webhook) *ClerkHook {
	return &ClerkHook{
		appClient: appClient,
		wh:        wh,
	}
}
