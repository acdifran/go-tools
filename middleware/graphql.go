package middleware

import (
	"context"

	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/acdifran/go-tools/viewer"
	clerkjwt "github.com/clerk/clerk-sdk-go/v2/jwt"
)

func WebsocketInit(
	loggedOutVC func(ctx context.Context) context.Context,
	newContextFromBase func(ctx context.Context, base *viewer.Context) context.Context,
) transport.WebsocketInitFunc {
	return func(
		ctx context.Context,
		initPayload transport.InitPayload,
	) (context.Context, *transport.InitPayload, error) {
		token := initPayload.Authorization()
		loggedOut := loggedOutVC(ctx)

		if token == "" {
			return loggedOut, &initPayload, nil
		}

		claims, err := clerkjwt.Verify(ctx, &clerkjwt.VerifyParams{
			Token: token,
			CustomClaimsConstructor: func(ctx context.Context) any {
				return &CustomClaims{}
			},
		})
		if err != nil {
			return loggedOut, &initPayload, nil
		}

		authContext := createAuthViewerContext(
			claims,
			"",
			"",
		)

		return newContextFromBase(ctx, authContext), &initPayload, nil
	}
}
