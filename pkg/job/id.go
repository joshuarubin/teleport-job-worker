package job

import "go.jetify.com/typeid"

// Prefix is used to define the job typeid prefix
type Prefix struct{}

// Prefix returns the job id prefix "job"
func (Prefix) Prefix() string { return "job" }

// ID is the job id type
type ID struct {
	typeid.TypeID[Prefix]
}

// NewID returns a new ID
func NewID() (ID, error) {
	return typeid.New[ID]()
}
