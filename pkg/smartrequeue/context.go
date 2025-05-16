package smartrequeue

import "context"

type contextKey struct{}

func NewContext(ctx context.Context, entry *Entry) context.Context {
	return context.WithValue(ctx, contextKey{}, entry)
}

func FromContext(ctx context.Context) *Entry {
	s, ok := ctx.Value(contextKey{}).(*Entry)
	if !ok {
		return nil
	}
	return s
}
