// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package attachcache

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// counter returns a loader answering val and counting its calls.
func counter(val string, calls *atomic.Int32) func(context.Context) (string, error) {
	return func(context.Context) (string, error) {
		calls.Add(1)

		return val, nil
	}
}

func TestASecondGetIsAHit(t *testing.T) {
	c := New(100, time.Minute)
	var calls atomic.Int32
	for range 3 {
		v, err := c.Get(t.Context(), "k", counter("abc", &calls))
		if err != nil || v != "abc" {
			t.Fatalf("got %q %v, want abc", v, err)
		}
	}

	if calls.Load() != 1 {
		t.Fatalf("loads %d, want 1", calls.Load())
	}
}

func TestAnEntryExpiresAfterItsTTL(t *testing.T) {
	clock := time.Unix(1000, 0)
	c := New(100, time.Minute)
	c.now = func() time.Time { return clock }

	var calls atomic.Int32
	_, _ = c.Get(t.Context(), "k", counter("abc", &calls))
	clock = clock.Add(59 * time.Second)
	_, _ = c.Get(t.Context(), "k", counter("abc", &calls))
	if calls.Load() != 1 {
		t.Fatalf("inside the TTL: loads %d, want 1", calls.Load())
	}

	clock = clock.Add(time.Second)
	_, _ = c.Get(t.Context(), "k", counter("abc", &calls))
	if calls.Load() != 2 {
		t.Fatalf("at the TTL: loads %d, want 2", calls.Load())
	}
}

func TestTheByteBoundEvictsTheLeastRecentlyUsed(t *testing.T) {
	c := New(10, time.Minute)
	var calls atomic.Int32
	_, _ = c.Get(t.Context(), "a", counter("aaaa", &calls))
	_, _ = c.Get(t.Context(), "b", counter("bbbb", &calls))

	// Touch a so b is the oldest, then c needs room.
	_, _ = c.Get(t.Context(), "a", counter("aaaa", &calls))
	_, _ = c.Get(t.Context(), "c", counter("cccc", &calls))
	if c.size > 10 {
		t.Fatalf("size %d over the bound", c.size)
	}

	if _, ok := c.lookup("b"); ok {
		t.Fatal("b survived, want it evicted as least recently used")
	}

	if _, ok := c.lookup("a"); !ok {
		t.Fatal("a evicted, want it kept as recently used")
	}

	if _, ok := c.lookup("c"); !ok {
		t.Fatal("c not stored")
	}
}

func TestAValueLargerThanTheBoundIsServedNotKept(t *testing.T) {
	c := New(10, time.Minute)
	var calls atomic.Int32
	big := strings.Repeat("x", 11)
	for range 2 {
		v, err := c.Get(t.Context(), "big", counter(big, &calls))
		if err != nil || v != big {
			t.Fatalf("got %d bytes %v, want the value served", len(v), err)
		}
	}

	if calls.Load() != 2 || c.size != 0 || len(c.items) != 0 {
		t.Fatalf("loads %d size %d items %d, want 2 loads and nothing kept", calls.Load(), c.size, len(c.items))
	}
}

func TestAnErrorIsNotKept(t *testing.T) {
	c := New(100, time.Minute)
	gone := errors.New("gone")
	calls := 0
	load := func(context.Context) (string, error) {
		calls++
		if calls == 1 {
			return "", gone
		}

		return "abc", nil
	}

	if _, err := c.Get(t.Context(), "k", load); !errors.Is(err, gone) {
		t.Fatalf("first: err %v, want the load error", err)
	}

	v, err := c.Get(t.Context(), "k", load)
	if err != nil || v != "abc" || calls != 2 {
		t.Fatalf("second: got %q %v after %d loads, want a fresh load", v, err, calls)
	}
}

func TestConcurrentMissesShareOneLoad(t *testing.T) {
	c := New(100, time.Minute)
	release := make(chan struct{})
	var calls atomic.Int32
	load := func(context.Context) (string, error) {
		calls.Add(1)
		<-release

		return "abc", nil
	}

	const n = 50
	var started, wg sync.WaitGroup
	started.Add(n)
	errs := make(chan error, n)
	for range n {
		wg.Go(func() {
			started.Done()
			v, err := c.Get(context.Background(), "k", load)
			if err == nil && v != "abc" {
				err = errors.New("wrong value " + v)
			}

			errs <- err
		})
	}

	started.Wait()
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	if calls.Load() != 1 {
		t.Fatalf("loads %d, want 1", calls.Load())
	}
}

func TestANilCacheLoadsEveryTime(t *testing.T) {
	var c *Cache
	var calls atomic.Int32
	for range 2 {
		if v, err := c.Get(t.Context(), "k", counter("abc", &calls)); err != nil || v != "abc" {
			t.Fatalf("got %q %v", v, err)
		}
	}

	if calls.Load() != 2 {
		t.Fatalf("loads %d, want 2", calls.Load())
	}
}
