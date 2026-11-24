package lru

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	f.mu.Unlock()
}

type eviction struct {
	key    string
	value  int
	reason Reason
}

func TestCapacityEvictsLeastRecentlyUsed(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	c := New[string, int](2, 0, clock.Now)
	var evicted []eviction
	c.OnEvict(func(k string, v int, r Reason) { evicted = append(evicted, eviction{k, v, r}) })

	c.Set("a", 1)
	c.Set("b", 2)
	if v, ok := c.Get("a"); !ok || v != 1 {
		t.Fatalf("Get(a) = %v, %v", v, ok)
	}
	c.Set("c", 3)
	if _, ok := c.Get("b"); ok {
		t.Fatal("b should have been evicted")
	}
	if got, want := c.Keys(), []string{"c", "a"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Keys() = %v, want %v", got, want)
	}
	if want := []eviction{{"b", 2, Capacity}}; !reflect.DeepEqual(evicted, want) {
		t.Fatalf("evictions = %v, want %v", evicted, want)
	}
}

func TestSetReplacesWithoutEviction(t *testing.T) {
	c := New[string, int](2, 0, time.Now)
	calls := 0
	c.OnEvict(func(string, int, Reason) { calls++ })
	c.Set("a", 1)
	c.Set("b", 2)
	c.Set("a", 10)
	if c.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", c.Len())
	}
	if v, _ := c.Get("a"); v != 10 {
		t.Fatalf("Get(a) = %d, want 10", v)
	}
	if got, want := c.Keys(), []string{"a", "b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Keys() = %v, want %v", got, want)
	}
	if calls != 0 {
		t.Fatalf("replacing a value fired %d evictions", calls)
	}
}

func TestDeleteDoesNotFireEviction(t *testing.T) {
	c := New[string, int](3, 0, time.Now)
	calls := 0
	c.OnEvict(func(string, int, Reason) { calls++ })
	c.Set("a", 1)
	if !c.Delete("a") {
		t.Fatal("Delete(a) = false")
	}
	if c.Delete("a") {
		t.Fatal("second Delete(a) = true")
	}
	if calls != 0 || c.Len() != 0 {
		t.Fatalf("calls=%d len=%d", calls, c.Len())
	}
}

func TestTTLExpiry(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	c := New[string, int](10, time.Minute, clock.Now)
	var evicted []eviction
	c.OnEvict(func(k string, v int, r Reason) { evicted = append(evicted, eviction{k, v, r}) })

	c.Set("a", 1)
	clock.Advance(30 * time.Second)
	c.Set("b", 2)
	if _, ok := c.Get("a"); !ok {
		t.Fatal("a expired too early")
	}
	clock.Advance(31 * time.Second)
	if _, ok := c.Get("a"); ok {
		t.Fatal("a should expire one minute after it was set, reads do not extend it")
	}
	if got, want := c.Keys(), []string{"b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Keys() = %v, want %v", got, want)
	}
	clock.Advance(30 * time.Second)
	if c.Len() != 0 {
		t.Fatalf("Len() = %d after everything expired", c.Len())
	}
	want := []eviction{{"a", 1, Expired}, {"b", 2, Expired}}
	if !reflect.DeepEqual(evicted, want) {
		t.Fatalf("evictions = %v, want %v", evicted, want)
	}
}

func TestSetRefreshesTTL(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	c := New[string, int](10, time.Minute, clock.Now)
	c.Set("a", 1)
	clock.Advance(50 * time.Second)
	c.Set("a", 2)
	clock.Advance(50 * time.Second)
	if v, ok := c.Get("a"); !ok || v != 2 {
		t.Fatalf("Get(a) = %v, %v; Set must restart the TTL", v, ok)
	}
}

func TestReasonString(t *testing.T) {
	if Capacity.String() != "capacity" || Expired.String() != "expired" {
		t.Fatalf("Reason strings = %q, %q", Capacity.String(), Expired.String())
	}
}

func TestConcurrentAccess(t *testing.T) {
	c := New[string, int](64, time.Hour, time.Now)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				k := fmt.Sprintf("k%d", (g*31+i)%100)
				c.Set(k, i)
				c.Get(k)
				if i%7 == 0 {
					c.Delete(k)
				}
				c.Keys()
			}
		}(g)
	}
	wg.Wait()
	if c.Len() > 64 {
		t.Fatalf("Len() = %d exceeds capacity", c.Len())
	}
}
