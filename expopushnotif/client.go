package expopushnotif

import (
	expo "github.com/oliveroneill/exponent-server-sdk-golang/sdk"
)

type ExpoPushNotif struct {
	client *expo.PushClient
}

func New() *ExpoPushNotif {
	client := expo.NewPushClient(nil)
	return &ExpoPushNotif{client}
}
