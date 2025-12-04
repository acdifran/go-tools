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
		return gqlerror.ErrorPathf(path, cerr.ClientMsg())
	} else if errors.Is(err, privacyDenyErr) {
		return gqlerror.ErrorPathf(path, "Permission denied")
	} else if isNotFound(err) {
		return gqlerror.ErrorPathf(path, "Not found")
	}

	// Unwrap the top level graphql error
	if errInternal := errors.Unwrap(err); errInternal != nil {
		err = errInternal
	}
	if errors.As(err, &gqlErr) {
		if gqlErr.Path == nil {
			gqlErr.Path = path
		}
		return gqlErr
	}

	return gqlerror.ErrorPathf(path, "Sorry, something went wrong")
}
