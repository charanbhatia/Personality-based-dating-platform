package cache

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestNoopAlwaysMisses(t *testing.T) {
	ctx := context.Background()
	var c Cache = Noop{}

	if err := c.Set(ctx, "k", []byte("v"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	value, found, err := c.Get(ctx, "k")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if found || value != nil {
		t.Fatalf("Get returned (%v, %v), want (nil, false)", value, found)
	}
	if err := c.Delete(ctx, "k"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestMemoryGetSetDelete(t *testing.T) {
	ctx := context.Background()
	c := NewMemory(10)

	if _, found, _ := c.Get(ctx, "missing"); found {
		t.Fatal("an unset key reported a hit")
	}
	if err := c.Set(ctx, "k", []byte("value"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	value, found, err := c.Get(ctx, "k")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !found || string(value) != "value" {
		t.Fatalf("Get returned (%q, %v)", value, found)
	}

	if err := c.Delete(ctx, "k"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, found, _ := c.Get(ctx, "k"); found {
		t.Fatal("the key survived Delete")
	}
}

func TestMemoryDeleteIsVariadicAndForgiving(t *testing.T) {
	ctx := context.Background()
	c := NewMemory(10)
	for _, k := range []string{"a", "b", "c"} {
		if err := c.Set(ctx, k, []byte(k), time.Minute); err != nil {
			t.Fatalf("Set: %v", err)
		}
	}
	if err := c.Delete(ctx, "a", "b", "never-existed"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if c.Len() != 1 {
		t.Fatalf("Len = %d, want 1", c.Len())
	}
	if err := c.Delete(ctx); err != nil {
		t.Fatalf("Delete with no keys: %v", err)
	}
}

func TestMemoryExpiresEntries(t *testing.T) {
	ctx := context.Background()
	c := NewMemory(10)
	base := time.Now()
	c.now = func() time.Time { return base }

	if err := c.Set(ctx, "k", []byte("v"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	c.now = func() time.Time { return base.Add(59 * time.Second) }
	if _, found, _ := c.Get(ctx, "k"); !found {
		t.Fatal("the entry expired early")
	}

	c.now = func() time.Time { return base.Add(time.Minute + time.Nanosecond) }
	if _, found, _ := c.Get(ctx, "k"); found {
		t.Fatal("an expired entry was served")
	}
	// A failed Get must also reclaim the slot.
	if c.Len() != 0 {
		t.Fatalf("Len = %d, want 0; the expired entry was not reclaimed", c.Len())
	}
}

func TestMemoryIgnoresNonPositiveTTL(t *testing.T) {
	ctx := context.Background()
	c := NewMemory(10)

	for _, ttl := range []time.Duration{0, -time.Second} {
		if err := c.Set(ctx, "k", []byte("v"), ttl); err != nil {
			t.Fatalf("Set: %v", err)
		}
		if _, found, _ := c.Get(ctx, "k"); found {
			t.Fatalf("ttl %v was stored; it would never expire correctly", ttl)
		}
	}
}

func TestMemoryCopiesValuesBothWays(t *testing.T) {
	ctx := context.Background()
	c := NewMemory(10)

	stored := []byte("original")
	if err := c.Set(ctx, "k", stored, time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	stored[0] = 'X'
	got, _, _ := c.Get(ctx, "k")
	if string(got) != "original" {
		t.Fatalf("mutating the caller's slice changed the cached value: %q", got)
	}

	got[0] = 'Y'
	again, _, _ := c.Get(ctx, "k")
	if string(again) != "original" {
		t.Fatalf("mutating a returned slice changed the cached value: %q", again)
	}
}

func TestMemoryRespectsTheEntryCap(t *testing.T) {
	ctx := context.Background()
	const max = 20
	c := NewMemory(max)

	for i := 0; i < max*3; i++ {
		if err := c.Set(ctx, fmt.Sprintf("key-%d", i), []byte("v"), time.Minute); err != nil {
			t.Fatalf("Set: %v", err)
		}
		if c.Len() > max {
			t.Fatalf("after %d writes Len = %d, which exceeds the cap of %d", i+1, c.Len(), max)
		}
	}
}

func TestMemoryEvictionPrefersExpiredEntries(t *testing.T) {
	ctx := context.Background()
	const max = 4
	c := NewMemory(max)
	base := time.Now()
	c.now = func() time.Time { return base }

	for i := 0; i < max; i++ {
		if err := c.Set(ctx, fmt.Sprintf("stale-%d", i), []byte("v"), time.Minute); err != nil {
			t.Fatalf("Set: %v", err)
		}
	}
	// Everything is now expired, so inserting must reclaim rather than sample.
	c.now = func() time.Time { return base.Add(2 * time.Minute) }
	if err := c.Set(ctx, "fresh", []byte("v"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if c.Len() != 1 {
		t.Fatalf("Len = %d, want 1 (only the fresh entry)", c.Len())
	}
	if _, found, _ := c.Get(ctx, "fresh"); !found {
		t.Fatal("the newly written entry was evicted")
	}
}

func TestMemoryOverwriteAtCapacityDoesNotEvict(t *testing.T) {
	ctx := context.Background()
	const max = 3
	c := NewMemory(max)
	for i := 0; i < max; i++ {
		if err := c.Set(ctx, fmt.Sprintf("key-%d", i), []byte("v"), time.Minute); err != nil {
			t.Fatalf("Set: %v", err)
		}
	}
	// Refreshing an existing key is not growth, so it must not trigger eviction.
	if err := c.Set(ctx, "key-1", []byte("updated"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if c.Len() != max {
		t.Fatalf("Len = %d, want %d", c.Len(), max)
	}
	got, found, _ := c.Get(ctx, "key-1")
	if !found || string(got) != "updated" {
		t.Fatalf("Get returned (%q, %v), want the updated value", got, found)
	}
}

func TestNewMemoryDefaultsTheCap(t *testing.T) {
	for _, size := range []int{0, -1} {
		if got := NewMemory(size).maxEntries; got != DefaultMaxEntries {
			t.Errorf("NewMemory(%d).maxEntries = %d, want %d", size, got, DefaultMaxEntries)
		}
	}
}

func TestMemoryIsSafeForConcurrentUse(t *testing.T) {
	ctx := context.Background()
	c := NewMemory(64)

	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 300; i++ {
				key := fmt.Sprintf("key-%d", i%50)
				switch i % 3 {
				case 0:
					_ = c.Set(ctx, key, []byte("value"), time.Minute)
				case 1:
					_, _, _ = c.Get(ctx, key)
				case 2:
					_ = c.Delete(ctx, key)
				}
			}
		}(worker)
	}
	wg.Wait()

	if c.Len() > 64 {
		t.Fatalf("Len = %d, which exceeds the cap", c.Len())
	}
}

func TestMemoryJanitorSweepsAndStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := NewMemory(100)
	// Offset rather than frozen: the janitor needs the clock to keep moving for
	// the entry to fall out of its TTL.
	c.now = func() time.Time { return time.Now().Add(time.Hour) }

	if err := c.Set(ctx, "k", []byte("v"), time.Millisecond); err != nil {
		t.Fatalf("Set: %v", err)
	}

	done := make(chan struct{})
	go func() {
		c.StartJanitor(ctx, 10*time.Millisecond)
		close(done)
	}()

	deadline := time.After(2 * time.Second)
	for c.Len() != 0 {
		select {
		case <-deadline:
			t.Fatal("the janitor did not sweep the expired entry")
		case <-time.After(5 * time.Millisecond):
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("StartJanitor did not return after the context was cancelled")
	}
}
