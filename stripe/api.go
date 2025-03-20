package stripe

import (
	"context"
	"fmt"

	"github.com/acdifran/go-tools/pulid"
	"github.com/samber/lo"
	"github.com/stripe/stripe-go/v78"
)

type OrderItem struct {
	ID    pulid.ID
	Name  string
	Price int
}

type PaymentSessionResponse struct {
	ID  string
	Url string
}

func (s *StripePaymentProcessor) CreateSession(
	accountID string,
	orderID pulid.ID,
	redirectUrl string,
	orderItems ...OrderItem,
) (*PaymentSessionResponse, error) {
	lineItems := lo.Map(
		orderItems,
		func(orderItem OrderItem, i int) *stripe.CheckoutSessionLineItemParams {
			return &stripe.CheckoutSessionLineItemParams{
				Quantity: stripe.Int64(1),
				PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
					Currency:   stripe.String("usd"),
					UnitAmount: stripe.Int64(int64(orderItem.Price)),
					ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
						Name:     &orderItem.Name,
						Metadata: map[string]string{"id": string(orderItem.ID)},
					},
				},
			}
		},
	)

	session, err := s.Client.CheckoutSessions.New(&stripe.CheckoutSessionParams{
		Customer:          &accountID,
		ClientReferenceID: stripe.String(string(orderID)),
		Mode:              stripe.String("payment"),
		LineItems:         lineItems,
		SuccessURL:        &redirectUrl,
		CancelURL:         &redirectUrl,
	})
	if err != nil {
		return nil, fmt.Errorf("creating stripe checkout session: %w", err)
	}

	return &PaymentSessionResponse{
		Url: session.URL,
		ID:  session.ID,
	}, nil
}

func (s *StripePaymentProcessor) CreateCustomer(ctx context.Context) (string, error) {
	customerInfo, err := s.AccountClient.GetAccountInfoForViewer(ctx)
	if err != nil {
		return "", fmt.Errorf("getting customer info: %w", err)
	}
	customer, err := s.Client.Customers.New(&stripe.CustomerParams{
		Name:     &customerInfo.Name,
		Email:    &customerInfo.Email,
		Metadata: map[string]string{"orgID": string(customerInfo.OrgID)},
	})
	if err != nil {
		return "", fmt.Errorf("creating stripe customer: %w", err)
	}

	return customer.ID, nil
}

func (s *StripePaymentProcessor) CreateAccount(isIndividual bool) (string, error) {
	businessType := "company"
	if isIndividual {
		businessType = "individual"
	}
	account, err := s.Client.Accounts.New(&stripe.AccountParams{
		Country: stripe.String("US"),
		Type:    stripe.String("express"),
		Capabilities: &stripe.AccountCapabilitiesParams{
			CardPayments: &stripe.AccountCapabilitiesCardPaymentsParams{
				Requested: stripe.Bool(true),
			},
			Transfers: &stripe.AccountCapabilitiesTransfersParams{
				Requested: stripe.Bool(true),
			},
		},
		BusinessType: stripe.String(businessType),
	})
	if err != nil {
		return "", fmt.Errorf("creating stripe customer: %w", err)
	}

	return account.ID, nil
}
