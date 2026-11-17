package middleware

import (
	"testing"
	"time"
)

func TestSlugCache_SetAndGet(t *testing.T) {
	cache := NewSlugCache(5 * time.Minute)
	defer cache.Stop()

	// Miss on empty cache
	if _, ok := cache.Get("my-workspace"); ok {
		t.Fatal("expected cache miss on empty cache")
	}

	// Set and get
	cache.Set("my-workspace", "uuid-123")
	uuid, ok := cache.Get("my-workspace")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if uuid != "uuid-123" {
		t.Fatalf("expected uuid-123, got %s", uuid)
	}
}

func TestSlugCache_Expiry(t *testing.T) {
	cache := NewSlugCache(50 * time.Millisecond)
	defer cache.Stop()

	cache.Set("my-workspace", "uuid-123")

	// Should hit before expiry
	if _, ok := cache.Get("my-workspace"); !ok {
		t.Fatal("expected cache hit before expiry")
	}

	// Wait for expiry
	time.Sleep(60 * time.Millisecond)

	// Should miss after expiry
	if _, ok := cache.Get("my-workspace"); ok {
		t.Fatal("expected cache miss after expiry")
	}
}

func TestSlugCache_Overwrite(t *testing.T) {
	cache := NewSlugCache(5 * time.Minute)
	defer cache.Stop()

	cache.Set("my-workspace", "uuid-old")
	cache.Set("my-workspace", "uuid-new")

	uuid, ok := cache.Get("my-workspace")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if uuid != "uuid-new" {
		t.Fatalf("expected uuid-new, got %s", uuid)
	}
}

func TestSlugCache_ConcurrentAccess(t *testing.T) {
	cache := NewSlugCache(5 * time.Minute)
	defer cache.Stop()

	done := make(chan bool, 100)

	// Concurrent writes
	for i := 0; i < 50; i++ {
		go func(n int) {
			cache.Set("slug", "uuid")
			done <- true
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 50; i++ {
		go func() {
			cache.Get("slug")
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 100; i++ {
		<-done
	}
}
