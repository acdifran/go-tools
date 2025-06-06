package pulid

import (
	"crypto/rand"
	"database/sql/driver"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/oklog/ulid/v2"
)

// ID implements a PULID - a prefixed ULID.
type ID string

// The default entropy source.
var defaultEntropySource *ulid.MonotonicEntropy

func init() {
	// Seed the default entropy source.
	// TODO: To improve testability, this package should allow control of entropy sources and the time.Now implementation.
	defaultEntropySource = ulid.Monotonic(rand.Reader, 0)
}

// newULID returns a new ULID for time.Now() using the default entropy source.
func newULID() ulid.ULID {
	return ulid.MustNew(ulid.Timestamp(time.Now()), defaultEntropySource)
}

// MustNew returns a new PULID for time.Now() given a prefix. This uses the default entropy source.
func MustNew(prefix string) ID { return ID(prefix + fmt.Sprint(newULID())) }

// UnmarshalGQL implements the graphql.Unmarshaler interface
func (u *ID) UnmarshalGQL(v any) error {
	return u.Scan(v)
}

// MarshalGQL implements the graphql.Marshaler interface
func (u ID) MarshalGQL(w io.Writer) {
	_, _ = io.WriteString(w, strconv.Quote(string(u)))
}

// Scan implements the Scanner interface.
func (u *ID) Scan(src interface{}) error {
	if src == nil {
		return fmt.Errorf("pulid: expected a value")
	}
	switch src := src.(type) {
	case string:
		*u = ID(src)
	case ID:
		*u = src
	default:
		return fmt.Errorf("pulid: unexpected type, %T", src)
	}
	return nil
}

// Value implements the driver Valuer interface.
func (u ID) Value() (driver.Value, error) {
	return string(u), nil
}
