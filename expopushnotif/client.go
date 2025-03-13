package expopushnotif

import (
	expo "github.com/oliveroneill/exponent-server-sdk-golang/sdk"
)

type ExpoPushNotifClient struct {
	client *expo.PushClient
}

func NewExpoPushNotifClient() *ExpoPushNotifClient {
	client := expo.NewPushClient(nil)
	return &ExpoPushNotifClient{client}
}
