package common

import "github.com/acdifran/go-tools/pulid"

type AccountInfo struct {
	OrgID pulid.ID
	Name  string
	Email string
}

type PaymentOrderItem struct {
	ID    pulid.ID
	Name  string
	Price int
}

type PaymentSessionResponse struct {
	ID  string
	Url string
}

type PresignedUrlObj struct {
	Key string `json:"key"`
	URL string `json:"url"`
}
