package tagaccess

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"sync"
)

// FixtureDeliveryVerifier is an explicit test adapter. Production callers must
// inject an ingress verifier that authenticates the VIBES authority envelope;
// AccessGate has no permissive default.
type FixtureDeliveryVerifier struct {
	mu      sync.RWMutex
	trusted map[[32]byte]struct{}
}

func NewFixtureDeliveryVerifier() *FixtureDeliveryVerifier {
	return &FixtureDeliveryVerifier{trusted: make(map[[32]byte]struct{})}
}

func (v *FixtureDeliveryVerifier) Trust(delivery ProjectionDelivery) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.trusted[verificationDigest(delivery)] = struct{}{}
}

func (v *FixtureDeliveryVerifier) Verify(_ context.Context, delivery ProjectionDelivery) error {
	if delivery.Kind == DeliveryIncremental {
		return nil
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	if _, trusted := v.trusted[verificationDigest(delivery)]; !trusted {
		return ErrUnverifiedDelivery
	}
	return nil
}

func verificationDigest(delivery ProjectionDelivery) [32]byte {
	payload, err := json.Marshal(delivery)
	if err != nil {
		return [32]byte{}
	}
	return sha256.Sum256(payload)
}
