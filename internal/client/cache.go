package client

import (
	"context"
	"fmt"
	"sync"
)

// cacheEntry holds the result for a cached key.
type cacheEntry struct {
	once  sync.Once
	value any
	err   error
}

// LoadCached returns the cached result for key, running load at most once per
// successful result per Client. Failed loads are not memoized: the entry is
// removed so a later call can retry (e.g. after a transient API or network error).
func LoadCached[T any](apiClient *Client, ctx context.Context, key string, load func(context.Context) (T, error)) (T, error) {
	var empty T
	actual, _ := apiClient.cache.LoadOrStore(key, &cacheEntry{})
	entry, ok := actual.(*cacheEntry)
	if !ok {
		return empty, fmt.Errorf("aikido cache: unexpected entry type for key %q", key)
	}

	entry.once.Do(func() {
		entry.value, entry.err = load(ctx)
	})

	if entry.err != nil {
		// Drop the failed entry so the next caller can retry. CompareAndDelete
		// avoids wiping a newer successful entry that may have replaced ours.
		apiClient.cache.CompareAndDelete(key, entry)
		return empty, entry.err
	}

	value, ok := entry.value.(T)
	if !ok {
		return empty, fmt.Errorf("aikido cache: unexpected value type for key %q", key)
	}

	return value, nil
}

// InvalidateCached drops the cached result for key so the next LoadCached runs load again.
func InvalidateCached(apiClient *Client, key string) {
	apiClient.cache.Delete(key)
}
