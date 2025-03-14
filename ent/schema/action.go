package schema

type ActionType string

const (
	ActionCreate ActionType = "Create"
	ActionUpdate ActionType = "Update"
)

type ActionDef struct {
	Name           string
	WrapTxn        bool
	Type           ActionType
	NoArgs         bool
	NillableArgs   bool
	DocComment     string
	GenericEntType string
	IsDefault      bool
}

type ActionOption func(*ActionDef)

func NewAction(name string, actionType ActionType, setters ...ActionOption) *ActionDef {
	action := &ActionDef{
		Name:    name,
		WrapTxn: true,
		Type:    actionType,
	}

	for _, setter := range setters {
		setter(action)
	}

	return action
}

func WrapTxn(wrap bool) ActionOption {
	return func(a *ActionDef) {
		a.WrapTxn = wrap
	}
}

func DocComment(comment string) ActionOption {
	return func(a *ActionDef) {
		a.DocComment = comment
	}
}

func IsDefault() ActionOption {
	return func(a *ActionDef) {
		a.IsDefault = true
	}
}

func NoArgs() ActionOption {
	return func(a *ActionDef) {
		a.NoArgs = true
	}
}

func NillableArgs() ActionOption {
	return func(a *ActionDef) {
		a.NillableArgs = true
	}
}

func GenericEntType(entType string) ActionOption {
	return func(a *ActionDef) {
		a.GenericEntType = entType
	}
}

type EntWithActions interface {
	Actions() []*ActionDef
}
