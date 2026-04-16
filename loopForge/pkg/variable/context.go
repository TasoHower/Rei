package variable

import "context"

type ctxKey struct{}

// NewContext returns a child context that carries store for FromContext.
func NewContext(parent context.Context, store *VarStore) context.Context {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithValue(parent, ctxKey{}, store)
}

// FromContext returns the VarStore attached to ctx, or nil if none.
func FromContext(ctx context.Context) *VarStore {
	if ctx == nil {
		return nil
	}
	s, _ := ctx.Value(ctxKey{}).(*VarStore)
	return s
}
