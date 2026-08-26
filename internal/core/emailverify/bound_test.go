// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package emailverify

import (
	"fmt"
	"testing"
	"time"
)

// The MX cache is bounded like the result cache. A caller with a
// wildcard zone can name a fresh domain per request, and this is the
// one read-tier route that grows memory per distinct input.
func TestTheMXCacheIsBounded(t *testing.T) {
	v := New(Config{})
	now := time.Now()
	for i := 0; i < maxCacheEntries*2; i++ {
		v.storeMX(fmt.Sprintf("d%d.example", i), true, now)
	}

	v.mu.RLock()
	n := len(v.mxAnswer)
	v.mu.RUnlock()
	if n > maxCacheEntries {
		t.Fatalf("mx cache holds %d entries, want at most %d", n, maxCacheEntries)
	}
}
