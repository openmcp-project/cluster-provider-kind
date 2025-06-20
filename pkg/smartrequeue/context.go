package smartrequeue

import "context"

// contextKey is a type used as a key for storing and retrieving the Entry from the context.
type contextKey struct{}

// NewContext creates a new context with the given Entry.
func NewContext(ctx context.Context, entry *Entry) context.Context {
	return context.WithValue(ctx, contextKey{}, entry)
}

// FromContext retrieves the Entry from the context, if it exists.
func FromContext(ctx context.Context) *Entry {
	s, ok := ctx.Value(contextKey{}).(*Entry)
	if !ok {
		return nil
	}
	return s
}
