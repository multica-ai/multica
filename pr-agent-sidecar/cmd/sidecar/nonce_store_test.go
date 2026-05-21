package main

import (
	"sync"
	"testing"
	"time"
)

func TestNonceStore_PutConsumeRoundTrip(t *testing.T) {
	s := NewNonceStore(5 * time.Minute)
	defer s.Close()

	want := PRContext{InstallationID: 42, Owner: "zoopone", Repo: "multica", PRNumber: 7, HeadSHA: "abc"}
	nonce, err := s.Put(want)
	if err != nil {
		t.Fatal(err)
	}
	if len(nonce) != 32 {
		t.Fatalf("nonce length = %d, want 32", len(nonce))
	}

	got, ok := s.Consume(nonce)
	if !ok {
		t.Fatal("consume returned ok=false on fresh nonce")
	}
	if got != want {
		t.Fatalf("consume returned %+v, want %+v", got, want)
	}
}

func TestNonceStore_SingleUse(t *testing.T) {
	s := NewNonceStore(5 * time.Minute)
	defer s.Close()

	nonce, _ := s.Put(PRContext{InstallationID: 1})
	if _, ok := s.Consume(nonce); !ok {
		t.Fatal("first consume should succeed")
	}
	if _, ok := s.Consume(nonce); ok {
		t.Fatal("second consume should fail (single-use)")
	}
}

func TestNonceStore_Expired(t *testing.T) {
	s := NewNonceStore(20 * time.Millisecond)
	defer s.Close()

	nonce, _ := s.Put(PRContext{InstallationID: 1})
	time.Sleep(40 * time.Millisecond)

	if _, ok := s.Consume(nonce); ok {
		t.Fatal("expired nonce should not be consumable")
	}
}

func TestNonceStore_SweepRemovesExpired(t *testing.T) {
	s := NewNonceStore(10 * time.Millisecond)
	defer s.Close()

	for i := 0; i < 5; i++ {
		if _, err := s.Put(PRContext{InstallationID: int64(i)}); err != nil {
			t.Fatal(err)
		}
	}
	if s.Len() != 5 {
		t.Fatalf("Len = %d, want 5", s.Len())
	}

	time.Sleep(30 * time.Millisecond)
	s.sweepOnce(time.Now())

	if got := s.Len(); got != 0 {
		t.Fatalf("after sweep Len = %d, want 0", got)
	}
}

func TestNonceStore_ConcurrentPutConsume(t *testing.T) {
	s := NewNonceStore(5 * time.Minute)
	defer s.Close()

	const n = 200
	nonces := make(chan string, n)

	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			nonce, err := s.Put(PRContext{InstallationID: int64(i)})
			if err != nil {
				t.Errorf("put: %v", err)
				return
			}
			nonces <- nonce
		}()
	}
	wg.Wait()
	close(nonces)

	seen := map[int64]bool{}
	for nonce := range nonces {
		ctx, ok := s.Consume(nonce)
		if !ok {
			t.Error("consume failed on freshly-put nonce")
			continue
		}
		seen[ctx.InstallationID] = true
	}
	if len(seen) != n {
		t.Fatalf("saw %d distinct installations, want %d", len(seen), n)
	}
}
