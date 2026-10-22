package main

import (
	"context"
	"sync"
)

// listMemo remembers successful listings for the length of one scan, so the workers scan's
// binding resolution and the storage and data scans don't list the same account twice. It
// lives in the scan's context: BatchScan shares one across services, and each standalone
// ScanService gets its own. Failed or partial listings are never remembered.
type listMemo struct {
	mu      sync.Mutex
	entries map[string]any
}

type listMemoKey struct{}

// withListMemo returns ctx with a listMemo, keeping one that is already there.
func withListMemo(ctx context.Context) context.Context {
	if listMemoFrom(ctx) != nil {
		return ctx
	}
	return context.WithValue(ctx, listMemoKey{}, &listMemo{entries: map[string]any{}})
}

func listMemoFrom(ctx context.Context) *listMemo {
	memo, _ := ctx.Value(listMemoKey{}).(*listMemo)
	return memo
}

// memoizedList returns the listing remembered under key, or calls fetch and remembers what it
// returns when it succeeds. Without a memo in ctx it just calls fetch.
func memoizedList[T any](ctx context.Context, key string, fetch func() ([]T, error)) ([]T, error) {
	memo := listMemoFrom(ctx)
	if memo == nil {
		return fetch()
	}
	memo.mu.Lock()
	cached, ok := memo.entries[key]
	memo.mu.Unlock()
	if ok {
		return cached.([]T), nil
	}
	items, err := fetch()
	if err == nil {
		memo.mu.Lock()
		memo.entries[key] = items
		memo.mu.Unlock()
	}
	return items, err
}
