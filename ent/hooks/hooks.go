package hooks

import (
	"context"
	"log/slog"

	"entgo.io/ent"
	"github.com/acdifran/go-tools/logger"
)

func EntMutationLogDecorator() func(next ent.Mutator) ent.Mutator {
	return func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			val, err := next.Mutate(ctx, m)
			if err != nil {
				return nil, logger.WrapError(
					err,
					slog.Group("ent_mutation", logger.EntMutationAttrs(m)...),
				)
			}

			return val, err
		})
	}
}
