package stripe

import (
	"context"

	"github.com/acdifran/go-tools/common"
	"github.com/stripe/stripe-go/v78/client"
)

type AccountProvider interface {
	GetAccountInfoForViewer(ctx context.Context) (*common.AccountInfo, error)
}

type StripePaymentProcessor struct {
	Client        *client.API
	ClientUrl     string
	AccountClient AccountProvider
}

func NewStripePaymentProcessor(
	key string,
	clientUrl string,
	accountClient AccountProvider,
) *StripePaymentProcessor {
	sc := &client.API{}
	sc.Init(key, nil)
	return &StripePaymentProcessor{Client: sc, ClientUrl: clientUrl, AccountClient: accountClient}
}
