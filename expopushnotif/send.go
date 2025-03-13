package expopushnotif

import (
	"fmt"
	"log/slog"
	"sync"

	expo "github.com/oliveroneill/exponent-server-sdk-golang/sdk"
)

func (e *ExpoPushNotifClient) SendPushNotif(
	recipientTokens []string,
	title string,
	body string,
	data map[string]string,
) {
	var wg sync.WaitGroup
	for _, rt := range recipientTokens {
		wg.Add(1)
		go func(rt string) {
			defer wg.Done()
			pushToken, err := expo.NewExponentPushToken(rt)
			if err != nil {
				slog.Error(
					fmt.Errorf("getting push token: %w", err).
						Error(),
				)
			}

			response, err := e.client.Publish(
				&expo.PushMessage{
					To:       []expo.ExponentPushToken{pushToken},
					Title:    title,
					Body:     body,
					Data:     data,
					Sound:    "default",
					Priority: expo.DefaultPriority,
				},
			)
			if err != nil {
				slog.Error(
					fmt.Errorf("sending push notif: %w", err).
						Error(),
				)
			}
			if response.ValidateResponse() != nil {
				fmt.Println()
				slog.Error(fmt.Sprintf("push notif failed for: %s", response.PushMessage.To))
			}
		}(rt)
	}
}
