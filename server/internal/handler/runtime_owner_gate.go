package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/redis/go-redis/v9"
)

const runtimeOwnerGatePrefix = "mul:{runtime_pending}:runtime:owner:"

var advanceRuntimeOwnerScript = redis.NewScript(`
local current = redis.call('GET', KEYS[1])
if current ~= ARGV[1] then
    if current and string.sub(current, 1, 1) == 'g' and
        (string.sub(ARGV[1], 1, 1) ~= 'g' or
         string.match(ARGV[1], '^[^:]+') <= string.match(current, '^[^:]+')) then
        return 0
    end
    redis.call('SET', KEYS[1], ARGV[1])
end
return 1
`)

var touchRuntimeOwnerScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) ~= ARGV[1] then return 0 end
redis.call('SET', KEYS[2], ARGV[1], 'PX', ARGV[2])
return 1
`)

// The same logical target is known before Register creates its runtime row.
func runtimeOwnerGateKey(workspaceID, daemonID, provider, profileID string) string {
	identity := workspaceID + "\x00" + daemonID + "\x00" + provider
	if profileID != "" {
		identity = workspaceID + "\x00" + daemonID + "\x00profile\x00" + profileID
	}
	key := sha256.Sum256([]byte(identity))
	return runtimeOwnerGatePrefix + hex.EncodeToString(key[:])
}

type pendingOwnerKey struct{}

type pendingOwner struct {
	gate       *localRuntimeOwnerGate
	key        string
	generation string
}

func withPendingOwner(ctx context.Context, gate *localRuntimeOwnerGate, key, generation string) context.Context {
	return context.WithValue(ctx, pendingOwnerKey{}, pendingOwner{gate: gate, key: key, generation: generation})
}

func ownerFromContext(ctx context.Context) pendingOwner {
	owner, _ := ctx.Value(pendingOwnerKey{}).(pendingOwner)
	return owner
}

func redisPendingOwner(ctx context.Context) (string, string) {
	owner := ownerFromContext(ctx)
	if owner.key == "" {
		return runtimeOwnerGatePrefix + "unused", "@bypass"
	}
	return owner.key, owner.generation
}

func pendingClaimError(result int64) error {
	if result == -1 {
		return service.ErrStaleRuntimeOwner
	}
	return nil
}

// Used only when Redis is not configured and pending stores are process-local.
type localRuntimeOwnerGate struct {
	mu         sync.RWMutex
	generation map[string]string
}

func (g *localRuntimeOwnerGate) advance(key, generation string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.generation == nil {
		g.generation = make(map[string]string)
	}
	current := g.generation[key]
	if current != generation && strings.HasPrefix(current, "g") &&
		(!strings.HasPrefix(generation, "g") || strings.SplitN(generation, ":", 2)[0] <= strings.SplitN(current, ":", 2)[0]) {
		return service.ErrStaleRuntimeOwner
	}
	g.generation[key] = generation
	return nil
}

func (g *localRuntimeOwnerGate) current(key, generation string) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.generation[key] == generation
}

func lockPendingOwner(ctx context.Context) (func(), error) {
	owner := ownerFromContext(ctx)
	if owner.gate == nil {
		return func() {}, nil
	}
	owner.gate.mu.RLock()
	if owner.gate.generation[owner.key] != owner.generation {
		owner.gate.mu.RUnlock()
		return nil, service.ErrStaleRuntimeOwner
	}
	return owner.gate.mu.RUnlock, nil
}

func (h *Handler) advanceRuntimeOwner(ctx context.Context, key, generation string) error {
	if store, ok := h.LivenessStore.(*RedisLivenessStore); ok && store.Available() {
		result, err := advanceRuntimeOwnerScript.Run(ctx, store.rdb, []string{key}, generation).Int64()
		if err != nil {
			return fmt.Errorf("advance runtime owner in Redis: %w", err)
		}
		if result == 0 {
			return service.ErrStaleRuntimeOwner
		}
		return nil
	}
	return h.runtimeOwnerGate.advance(key, generation)
}

func (h *Handler) touchRuntimeOwner(ctx context.Context, runtimeID, key, generation string) error {
	if store, ok := h.LivenessStore.(*RedisLivenessStore); ok && store.Available() {
		result, err := touchRuntimeOwnerScript.Run(ctx, store.rdb,
			[]string{key, runtimeLivenessKey(runtimeID)}, generation, runtimeLivenessTTL.Milliseconds()).Int64()
		if err != nil {
			return fmt.Errorf("touch runtime liveness: %w", err)
		}
		if result == 0 {
			return service.ErrStaleRuntimeOwner
		}
		return nil
	}
	if _, realStore := h.LivenessStore.(*RedisLivenessStore); !realStore {
		return h.LivenessStore.Touch(ctx, runtimeID, runtimeLivenessTTL)
	}
	h.runtimeOwnerGate.mu.RLock()
	defer h.runtimeOwnerGate.mu.RUnlock()
	if h.runtimeOwnerGate.generation[key] != generation {
		return service.ErrStaleRuntimeOwner
	}
	return h.LivenessStore.Touch(ctx, runtimeID, runtimeLivenessTTL)
}

func writeRuntimeOwnerAdvanceError(w http.ResponseWriter, err error) {
	if err == service.ErrStaleRuntimeOwner {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeError(w, http.StatusServiceUnavailable, "runtime ownership gate unavailable")
}
