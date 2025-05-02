package centrifugomessenger

import (
	"crypto/tls"
	"log"

	"github.com/acdifran/go-tools/centrifugomessenger/apiproto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

type CentrifugoMessenger struct {
	client apiproto.CentrifugoApiClient
}

func New(endpoint string, enableTLS bool) *CentrifugoMessenger {
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
	return &CentrifugoMessenger{apiproto.NewCentrifugoApiClient(conn)}
}
