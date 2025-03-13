package membershiprole

import (
	"fmt"
	"io"
	"log/slog"
	"strconv"
)

type MembershipRole string

const (
	Admin  MembershipRole = "ADMIN"
	Member MembershipRole = "MEMBER"
)

func (MembershipRole) Values() (kinds []string) {
	for _, s := range []MembershipRole{Admin, Member} {
		kinds = append(kinds, string(s))
	}
	return
}

func (p MembershipRole) String() string {
	return string(p)
}

func (p MembershipRole) MarshalGQL(w io.Writer) {
	_, err := io.WriteString(w, strconv.Quote(p.String()))
	if err != nil {
		err = fmt.Errorf("marshalling MembershipRole: %w", err)
		slog.Error(err.Error())
	}
}

func (_g *MembershipRole) UnmarshalGQL(val any) error {
	str, ok := val.(string)
	if !ok {
		return fmt.Errorf("enum %T must be a string", val)
	}
	*_g = MembershipRole(str)

	switch *_g {
	case Admin, Member:
		return nil
	default:
		return fmt.Errorf("%s is not a valid MembershipRole", str)
	}
}

func Coerce(val any) (MembershipRole, error) {
	str, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("enum %T must be a string", val)
	}
	e := MembershipRole(str)

	switch e {
	case Admin, Member:
		return e, nil
	default:
		return "", fmt.Errorf("%s is not a valid MembershipRole", str)
	}
}
