package logger

import "log/slog"

// ErrorWithAttrs is an error that carries structured log attributes.
// Attributes are attached as the error bubbles up the call stack and
// recovered at the top with ExtractAttrs.
type ErrorWithAttrs struct {
	err   error
	attrs []slog.Attr
}

func (e *ErrorWithAttrs) Error() string {
	return e.err.Error()
}

func (e *ErrorWithAttrs) Unwrap() error {
	return e.err
}

// WrapError wraps an error with log attributes. The attributes are recovered
// with ExtractAttrs, which walks the whole error chain — so wrapping an
// already-wrapped error simply adds another layer. Nothing is merged or
// flattened, which keeps every intermediate wrapper (and its message) intact.
func WrapError(err error, attrs ...slog.Attr) error {
	if err == nil {
		return nil
	}

	return &ErrorWithAttrs{
		err:   err,
		attrs: attrs,
	}
}

// maxUnwrapDepth bounds the error-chain traversal in ExtractAttrs. It guards
// against pathological or cyclic error chains; real chains are far shallower.
const maxUnwrapDepth = 100

// ExtractAttrs collects every log attribute attached anywhere in an error
// chain. It traverses both single-error wrappers (Unwrap() error) and
// multi-error wrappers such as errors.Join (Unwrap() []error). Each
// ErrorWithAttrs node contributes its attributes exactly once, even when the
// same node is reachable through multiple join branches.
func ExtractAttrs(err error) []slog.Attr {
	var attrs []slog.Attr
	seen := make(map[*ErrorWithAttrs]bool)

	var walk func(err error, depth int)
	walk = func(err error, depth int) {
		if err == nil || depth > maxUnwrapDepth {
			return
		}

		if e, ok := err.(*ErrorWithAttrs); ok {
			if seen[e] {
				return // already collected this node and its subtree
			}
			seen[e] = true
			attrs = append(attrs, e.attrs...)
		}

		// A type implements at most one Unwrap variant, so these cases are
		// mutually exclusive.
		switch x := err.(type) {
		case interface{ Unwrap() error }:
			walk(x.Unwrap(), depth+1)
		case interface{ Unwrap() []error }:
			for _, e := range x.Unwrap() {
				walk(e, depth+1)
			}
		}
	}
	walk(err, 0)

	return attrs
}
