package centrifugo

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/acdifran/go-tools/centrifugo/apiproto"
	"github.com/acdifran/go-tools/pulid"
)

type Message struct {
	Body any      `json:"body"`
	ID   pulid.ID `json:"id"`
	Type string   `json:"type"`
}

func (c *CentrifugoClient) SendMessage(
	ctx context.Context,
	message *Message,
	recipients []string,
) error {
	data, err := newPayload(message)
	if err != nil {
		return fmt.Errorf("failed to create payload: %w", err)
	}

	return c.sendMessage(
		ctx,
		recipients,
		data,
		fmt.Sprintf("message_%s_%s:%s", message.Type, message.ID, time.Now()),
	)
}

func newPayload(message *Message) ([]byte, error) {
	data, err := json.Marshal(message)
	if err != nil {
		return nil, fmt.Errorf("marshalling payload: %w", err)
	}
	return data, nil
}

func (c *CentrifugoClient) sendMessage(
	ctx context.Context,
	recipients []string,
	data []byte,
	idepmotencyKey string,
) error {
	channels := getPersonalChannels(recipients)
	if len(channels) == 0 {
		return nil
	}

	resp, err := c.client.Broadcast(ctx, &apiproto.BroadcastRequest{
		Channels:       channels,
		Data:           data,
		IdempotencyKey: fmt.Sprintf("message_%s", idepmotencyKey),
	})
	if err != nil {
		return fmt.Errorf("broadcasting message: %w", err)
	}

	respError := resp.GetError()
	if respError != nil {
		return fmt.Errorf("broadcasting message: %d - %s", respError.Code, respError.Message)
	}

	return nil
}

func getPersonalChannels(recipients []string) []string {
	channels := make([]string, 0, len(recipients))
	for _, r := range recipients {
		name := fmt.Sprintf("personal:#%s", r)
		channels = append(channels, name)
	}
	return channels
}
