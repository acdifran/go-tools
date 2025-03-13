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
	if errInternal := errors.Unwrap(err); errInternal != nil {
		err = errInternal
	}

	defer func() {
		var cerr *clienterror.Error
		if errors.As(err, &cerr) {
			return
		}
		if errors.Is(err, privacyDenyErr) {
			gqlErr.Message = "Permission denied"
		}
		if isNotFound(err) {
			gqlErr.Message = "Not found"
		}
	}()

	path := graphql.GetPath(ctx)
	if errors.As(err, &gqlErr) {
		if gqlErr.Path == nil {
			gqlErr.Path = path
		}

		return gqlErr
	}

	var cerr *clienterror.Error
	if errors.As(err, &cerr) {
		return gqlerror.ErrorPathf(path, "%s", cerr.ClientMsg())
	}

	return gqlerror.ErrorPathf(path, "Sorry, something went wrong")
}
