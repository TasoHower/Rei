package skill

import "errors"

// ErrNotFound is returned when a skill name is not present in the registry.
var ErrNotFound = errors.New("skill: not found")
