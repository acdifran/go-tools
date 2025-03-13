package centrifugo

import (
	"crypto/tls"
	"log"

	"github.com/acdifran/go-tools/centrifugo/apiproto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

type CentrifugoClient struct {
	client apiproto.CentrifugoApiClient
}

func NewCentrifugoClient(endpoint string, enableTLS bool) *CentrifugoClient {
	creds := insecure.NewCredentials()

	if enableTLS {
		tlsConfig := &tls.Config{
			InsecureSkipVerify: false,
		}
		creds = credentials.NewTLS(tlsConfig)
	}

	conn, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(creds))
	if err != nil {
		log.Fatalf("could not initialize centrifugo client: %s", err.Error())
	}
	return &CentrifugoClient{apiproto.NewCentrifugoApiClient(conn)}
}
