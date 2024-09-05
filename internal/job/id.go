package job

import "go.jetify.com/typeid"

type Prefix struct{}

func (Prefix) Prefix() string { return "job" }

type ID struct {
	typeid.TypeID[Prefix]
}

func NewID() (ID, error) {
	return typeid.New[ID]()
}
