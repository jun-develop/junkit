package upload

import "errors"

var (
	// ErrInvalidFile is returned when an upload breaks its Rule.
	ErrInvalidFile = errors.New("upload: invalid file")
)
