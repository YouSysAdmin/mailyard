// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package attachcache keeps the content of template attachments in
// memory, so a campaign whose every message carries the same file reads
// it from the database or the object store once rather than once per
// recipient.
//
// The content of one template attachment id never changes: a new upload
// is a new row and a soft delete leaves the bytes alone. Nothing is
// invalidated, and an entry expires after a TTL only so that a row the
// retention sweep purged stops being served within that window.
package attachcache

import (
	"container/list"
	"context"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// DefaultTTL is how long an entry is served before it is loaded again.
const DefaultTTL = 10 * time.Minute

// Cache is a least recently used cache bounded by the total length of
// the values it holds. Values are strings, so a caller can never change
// what another caller reads. A nil Cache, or one with no room, loads on
// every call. Safe for concurrent use.
type Cache struct {
	max int64
	ttl time.Duration
	now func() time.Time

	mu    sync.Mutex
	size  int64
	order *list.List
	items map[string]*list.Element

	group singleflight.Group
}

type entry struct {
	key     string
	val     string
	expires time.Time
}

// New builds a Cache holding at most maxBytes of values, each for ttl.
func New(maxBytes int64, ttl time.Duration) *Cache {
	return &Cache{
		max:   maxBytes,
		ttl:   ttl,
		now:   time.Now,
		order: list.New(),
		items: map[string]*list.Element{},
	}
}

// Get answers key from the cache, or calls load and keeps its answer.
// Concurrent misses for one key share a single load. An error is never
// kept, so the next call loads again.
func (c *Cache) Get(ctx context.Context, key string, load func(context.Context) (string, error)) (string, error) {
	if c == nil || c.max <= 0 || c.ttl <= 0 {
		return load(ctx)
	}

	if v, ok := c.lookup(key); ok {
		return v, nil
	}

	ch := c.group.DoChan(key, func() (any, error) {
		// A load that finished between the lookup above and this call
		// has already stored the answer.
		if v, ok := c.lookup(key); ok {
			return v, nil
		}

		v, err := load(ctx)
		if err != nil {
			return "", err
		}

		c.store(key, v)

		return v, nil
	})

	select {
	case r := <-ch:
		if r.Err != nil {
			return "", r.Err
		}

		v, _ := r.Val.(string)

		return v, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// lookup returns a live entry and marks it recently used. An expired
// entry is dropped.
func (c *Cache) lookup(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return "", false
	}

	e, _ := el.Value.(*entry)
	if !c.now().Before(e.expires) {
		c.remove(el)

		return "", false
	}

	c.order.MoveToFront(el)

	return e.val, true
}

// store keeps a value, evicting the least recently used entries until
// it fits. A value larger than the whole budget is not kept.
func (c *Cache) store(key, val string) {
	n := int64(len(val))
	if n > c.max {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.remove(el)
	}

	for c.size+n > c.max {
		c.remove(c.order.Back())
	}

	c.items[key] = c.order.PushFront(&entry{key: key, val: val, expires: c.now().Add(c.ttl)})
	c.size += n
}

// remove drops one element. Called with the lock held.
func (c *Cache) remove(el *list.Element) {
	e, _ := el.Value.(*entry)
	c.order.Remove(el)
	delete(c.items, e.key)
	c.size -= int64(len(e.val))
}
