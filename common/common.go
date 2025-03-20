package common

import "github.com/acdifran/go-tools/pulid"

type AccountInfo struct {
	OrgID pulid.ID
	Name  string
	Email string
}
