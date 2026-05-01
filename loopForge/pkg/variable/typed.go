package variable

import (
	"fmt"

	lferrors "github.com/TasoHower/Rei/loopForge/pkg/errors"
)

// ErrNotPresent is returned by GetRequired when the key is missing or the
// stored value is not assignable to T.
//
// Deprecated: use [lferrors.ErrNotPresent] directly. This alias is kept for
// backward compatibility.
var ErrNotPresent = lferrors.ErrNotPresent

// Get returns the value for key as T when the dynamic type matches.
func Get[T any](s *VarStore, key string) (T, bool) {
	var zero T
	if s == nil {
		return zero, false
	}
	v, ok := s.Get(key)
	if !ok {
		return zero, false
	}
	tv, ok := v.(T)
	if !ok {
		return zero, false
	}
	return tv, true
}

// GetRequired returns the value for key as T, or an error wrapping [ErrNotPresent]
// when the key is missing or the value is not assignable to T.
func GetRequired[T any](s *VarStore, key string) (T, error) {
	var zero T
	v, ok := Get[T](s, key)
	if !ok {
		return zero, fmt.Errorf("%w for key %q", lferrors.ErrNotPresent, key)
	}
	return v, nil
}
