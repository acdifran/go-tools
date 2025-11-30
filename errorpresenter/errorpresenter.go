package errorpresenter

import (
	"context"
	"errors"

	"github.com/99designs/gqlgen/graphql"
	"github.com/acdifran/go-tools/clienterror"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

func ErrorPresenter(
	ctx context.Context,
	err error,
	privacyDenyErr error,
	isNotFound func(err error) bool,
) (gqlErr *gqlerror.Error) {
	path := graphql.GetPath(ctx)

	var cerr *clienterror.Error
	if errors.As(err, &cerr) {
		return gqlerror.ErrorPathf(path, "%s", cerr.ClientMsg())
	}

	if errors.As(err, &gqlErr) {
		if gqlErr.Path == nil {
			gqlErr.Path = path
		}

		if errors.Is(err, privacyDenyErr) {
			gqlErr.Message = "Permission denied"
		} else if isNotFound(err) {
			gqlErr.Message = "Not found"
		} else {
			gqlErr.Message = "Sorry, something went wrong"
		}

		return gqlErr
	}

	return gqlerror.ErrorPathf(path, "Sorry, something went wrong")
}
