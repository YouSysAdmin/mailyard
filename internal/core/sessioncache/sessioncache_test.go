// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package sessioncache

import (
	"testing"
	"time"
)

func TestMissThenHit(t *testing.T) {
	c := New()
	now := time.Now()
	if _, ok := c.Lookup("e225ddc5-d236-4b3d-8892-08db6fdc9696", now); ok {
		t.Fatal("empty cache must miss")
	}

	c.Store("e225ddc5-d236-4b3d-8892-08db6fdc9696", "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1", now.Add(time.Hour), now)
	uid, ok := c.Lookup("e225ddc5-d236-4b3d-8892-08db6fdc9696", now)
	if !ok || uid != "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1" {
		t.Errorf("Lookup = %q, %v", uid, ok)
	}
}

func TestEntryGoesStale(t *testing.T) {
	c := New()
	now := time.Now()
	c.Store("e225ddc5-d236-4b3d-8892-08db6fdc9696", "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1", now.Add(time.Hour), now)
	// Past the trust window the cache must defer to the database
	// rather than keep answering from memory - that window IS the
	// revocation lag.
	if _, ok := c.Lookup("e225ddc5-d236-4b3d-8892-08db6fdc9696", now.Add(TTL+time.Second)); ok {
		t.Error("entry must go stale after TTL")
	}
}

func TestExpiredSessionNeverHits(t *testing.T) {
	c := New()
	now := time.Now()
	// Freshly checked, but the session itself has already expired.
	c.Store("e225ddc5-d236-4b3d-8892-08db6fdc9696", "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1", now.Add(-time.Minute), now)
	if _, ok := c.Lookup("e225ddc5-d236-4b3d-8892-08db6fdc9696", now); ok {
		t.Error("a cached entry must not outlive the session it describes")
	}
}

func TestInvalidate(t *testing.T) {
	c := New()
	now := time.Now()
	c.Store("e225ddc5-d236-4b3d-8892-08db6fdc9696", "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1", now.Add(time.Hour), now)
	c.Invalidate("e225ddc5-d236-4b3d-8892-08db6fdc9696")
	if _, ok := c.Lookup("e225ddc5-d236-4b3d-8892-08db6fdc9696", now); ok {
		t.Error("invalidated entry must miss immediately, that is what makes a local revoke instant")
	}
}

func TestInvalidateAll(t *testing.T) {
	c := New()
	now := time.Now()
	c.Store("e225ddc5-d236-4b3d-8892-08db6fdc9696", "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1", now.Add(time.Hour), now)
	c.Store("f9e75329-92b2-43f3-8294-1c66bbccf4e7", "c3533f0f-413f-4604-8417-1bbdc59958ec", now.Add(time.Hour), now)
	c.InvalidateAll()
	for _, id := range []string{"e225ddc5-d236-4b3d-8892-08db6fdc9696", "f9e75329-92b2-43f3-8294-1c66bbccf4e7"} {
		if _, ok := c.Lookup(id, now); ok {
			t.Errorf("%s must be gone", id)
		}
	}
}

func TestNilCacheIsSafe(t *testing.T) {
	var c *Cache
	if _, ok := c.Lookup("e225ddc5-d236-4b3d-8892-08db6fdc9696", time.Now()); ok {
		t.Error("nil cache must miss")
	}

	c.Store("e225ddc5-d236-4b3d-8892-08db6fdc9696", "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1", time.Now(), time.Now())
	c.Invalidate("e225ddc5-d236-4b3d-8892-08db6fdc9696")
	c.InvalidateAll()
}

func TestEvictionKeepsTheMapBounded(t *testing.T) {
	c := New()
	base := time.Now()
	for i := range 100 {
		c.Store(string(rune('a'+i%26))+string(rune('0'+i/26)), "u", base.Add(time.Hour), base)
	}

	// A store more than a minute later triggers the sweep, which must
	// drop every entry now past TTL.
	later := base.Add(2 * time.Minute)
	c.Store("fresh", "u", later.Add(time.Hour), later)
	c.mu.RLock()
	n := len(c.entries)
	c.mu.RUnlock()
	if n != 1 {
		t.Errorf("entries after eviction = %d, want just the fresh one", n)
	}
}
