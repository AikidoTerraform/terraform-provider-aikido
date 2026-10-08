package client

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

// cacheEntry holds the result for a cached key. settled is published after the
// load returns, so a reader that is not going through once can tell a finished
// entry from one still loading, and read value in the right order.
type cacheEntry struct {
	once    sync.Once
	settled atomic.Bool
	value   any
	err     error
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
		entry.settled.Store(true)
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

// UpdateCached replaces the cached result for key with update(cached), so a write
// to one member of a cached collection does not discard the whole thing. When
// there is no settled value to patch the key is dropped instead, so a stale
// snapshot never outlives a write. update must return a new value rather than
// mutate the one it receives: readers hold the old value unsynchronized.
func UpdateCached[T any](apiClient *Client, key string, update func(T) T) {
	apiClient.cacheUpdate.Lock()
	defer apiClient.cacheUpdate.Unlock()

	cached, ok := settledValue[T](apiClient, key)
	if !ok {
		apiClient.cache.Delete(key)

		return
	}

	apiClient.cache.Store(key, settledEntry(update(cached)))
}

// settledValue reads the result of a finished load without joining it, so an
// absent key stays absent and a load in flight is left alone.
func settledValue[T any](apiClient *Client, key string) (T, bool) {
	var empty T

	actual, ok := apiClient.cache.Load(key)
	if !ok {
		return empty, false
	}
	entry, ok := actual.(*cacheEntry)
	if !ok || !entry.settled.Load() || entry.err != nil {
		return empty, false
	}

	value, ok := entry.value.(T)

	return value, ok
}

// settledEntry holds an already-computed value, so LoadCached returns it without
// running load.
func settledEntry(value any) *cacheEntry {
	entry := &cacheEntry{value: value}
	entry.once.Do(func() {})
	entry.settled.Store(true)

	return entry
}

// InvalidateCached drops the cached result for key so the next LoadCached runs
// load again. It takes the update lock, so it cannot land inside an update and be
// undone by it.
func InvalidateCached(apiClient *Client, key string) {
	apiClient.cacheUpdate.Lock()
	defer apiClient.cacheUpdate.Unlock()

	apiClient.cache.Delete(key)
}
