package job

// UserID is the user id type
type UserID string

// String returns the user id as a string
func (id UserID) String() string {
	return string(id)
}
