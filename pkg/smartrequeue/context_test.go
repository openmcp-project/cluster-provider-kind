package smartrequeue

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewContext(t *testing.T) {
	entry := &Entry{}
	ctx := NewContext(context.Background(), entry)

	if got := FromContext(ctx); got != entry {
		assert.Equal(t, entry, got, "Expected entry to be the same as the one set in context")
	}
}

func TestFromContext(t *testing.T) {
	entry := &Entry{}
	ctx := NewContext(context.Background(), entry)

	if got := FromContext(ctx); got != entry {

		assert.Equal(t, entry, got, "Expected entry to be the same as the one set in context")
	}

	if got := FromContext(context.Background()); got != nil {
		assert.Nil(t, got, "Expected nil when no entry is set in context")
	}
}
