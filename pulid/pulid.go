package pulid

import (
	"database/sql/driver"
	"fmt"
	"io"
	"strconv"

	"github.com/oklog/ulid/v2"
)

// ID implements a PULID - a prefixed ULID.
type ID string

// ulid.Make's default entropy is locked and monotonic; a bare ulid.Monotonic source corrupts its buffer when ids
// are created from several goroutines at once.
func MustNew(prefix string) ID { return ID(prefix + ulid.Make().String()) }

// UnmarshalGQL implements the graphql.Unmarshaler interface
func (u *ID) UnmarshalGQL(v any) error {
	return u.Scan(v)
}

// MarshalGQL implements the graphql.Marshaler interface
func (u ID) MarshalGQL(w io.Writer) {
	_, _ = io.WriteString(w, strconv.Quote(string(u)))
}

// Scan implements the Scanner interface.
func (u *ID) Scan(src any) error {
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

func Ptr(s string) *ID {
	if s == "" {
		return nil
	}
	id := ID(s)
	return &id
}
