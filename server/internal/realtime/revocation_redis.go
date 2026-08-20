package realtime

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/multica-ai/multica/server/internal/tagaccess"
	"github.com/redis/go-redis/v9"
)

const (
	revocationNamespace           = "multica:tag:ws:revocation:v1"
	revocationNodesKey            = revocationNamespace + ":nodes"
	revocationCommandStream       = revocationNamespace + ":commands"
	revocationCommandStreamMaxLen = int64(10000)
	revocationNodeTTL             = 1500 * time.Millisecond
	revocationHeartbeatPeriod     = 250 * time.Millisecond
	revocationReadBlock           = 250 * time.Millisecond
	revocationAckTTL              = time.Minute
)

func revocationAckKey(commandID string) string {
	return revocationNamespace + ":ack:" + commandID
}

type RedisConnectionCloseBroker struct {
	hub        *Hub
	writeRDB   *redis.Client
	readRDB    *redis.Client
	instanceID string
	authKey    []byte

	mu            sync.Mutex
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	leaseDeadline atomic.Pointer[time.Time]
	leaseFailed   atomic.Bool
}

func NewRedisConnectionCloseBroker(hub *Hub, writeRDB, readRDB *redis.Client, instanceID string, authenticationKey []byte) *RedisConnectionCloseBroker {
	if readRDB == nil {
		readRDB = writeRDB
	}
	return &RedisConnectionCloseBroker{
		hub: hub, writeRDB: writeRDB, readRDB: readRDB, instanceID: instanceID,
		authKey: append([]byte(nil), authenticationKey...),
	}
}

func (b *RedisConnectionCloseBroker) connectionsHealthy(ctx context.Context) bool {
	if b == nil || b.writeRDB == nil || b.readRDB == nil || b.hub == nil || b.instanceID == "" || len(b.authKey) < sha256.Size {
		return false
	}
	pingCtx, cancel := context.WithTimeout(ctx, revocationHeartbeatPeriod)
	defer cancel()
	if b.writeRDB.Ping(pingCtx).Err() != nil {
		return false
	}
	return b.readRDB == b.writeRDB || b.readRDB.Ping(pingCtx).Err() == nil
}

func (b *RedisConnectionCloseBroker) Healthy(ctx context.Context) bool {
	if !b.LeaseValid() || !b.connectionsHealthy(ctx) {
		return false
	}
	leaseCtx, cancel := context.WithTimeout(ctx, revocationHeartbeatPeriod)
	defer cancel()
	redisNow, err := b.writeRDB.Time(leaseCtx).Result()
	if err != nil {
		return false
	}
	expiresAt, err := b.writeRDB.ZScore(leaseCtx, revocationNodesKey, b.instanceID).Result()
	return err == nil && int64(expiresAt) > redisNow.UTC().UnixMilli()
}

// LeaseValid is a local, allocation-free serving fence checked on every
// inbound/outbound socket path. Once a boot misses its lease deadline it can
// never renew or resume serving old sockets, which makes remote expiry safe to
// treat as a fenced process rather than a fabricated close acknowledgement.
func (b *RedisConnectionCloseBroker) LeaseValid() bool {
	if b == nil || b.leaseFailed.Load() {
		return false
	}
	deadline := b.leaseDeadline.Load()
	if deadline == nil {
		return false
	}
	if !time.Now().Before(*deadline) {
		b.leaseFailed.Store(true)
		return false
	}
	return true
}

func (b *RedisConnectionCloseBroker) Start(ctx context.Context) error {
	if !b.connectionsHealthy(ctx) {
		return errors.New("realtime revocation broker unavailable")
	}
	if err := b.ensureConsumerGroup(ctx); err != nil {
		return err
	}
	b.mu.Lock()
	if b.cancel != nil {
		b.mu.Unlock()
		return nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	b.cancel = cancel
	b.mu.Unlock()
	if err := b.heartbeatOnce(runCtx); err != nil {
		b.Stop()
		return err
	}
	b.wg.Add(2)
	go func() {
		defer b.wg.Done()
		b.heartbeatLoop(runCtx)
	}()
	go func() {
		defer b.wg.Done()
		b.readLoop(runCtx)
	}()
	return nil
}

func (b *RedisConnectionCloseBroker) consumerGroup() string {
	if b == nil {
		return ""
	}
	return revocationNamespace + ":group:" + b.instanceID
}

func (b *RedisConnectionCloseBroker) ensureConsumerGroup(ctx context.Context) error {
	if b == nil || b.writeRDB == nil || b.instanceID == "" {
		return errors.New("realtime revocation broker unavailable")
	}
	err := b.writeRDB.XGroupCreateMkStream(ctx, revocationCommandStream, b.consumerGroup(), "$").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return err
	}
	return nil
}

func (b *RedisConnectionCloseBroker) Stop() {
	b.mu.Lock()
	cancel := b.cancel
	b.cancel = nil
	b.mu.Unlock()
	if cancel != nil {
		cancel()
		b.leaseFailed.Store(true)
		b.wg.Wait()
		b.hub.closeAllForBrokerShutdown()
		unregisterCtx, unregisterCancel := context.WithTimeout(context.Background(), revocationHeartbeatPeriod)
		_ = b.writeRDB.ZRem(unregisterCtx, revocationNodesKey, b.instanceID).Err()
		unregisterCancel()
	}
}

func (b *RedisConnectionCloseBroker) Wait() { b.wg.Wait() }

func (b *RedisConnectionCloseBroker) ActiveInstances(ctx context.Context) ([]string, error) {
	if b == nil || b.writeRDB == nil {
		return nil, errors.New("realtime revocation broker unavailable")
	}
	redisNow, err := b.writeRDB.Time(ctx).Result()
	if err != nil {
		return nil, err
	}
	nowMillis := redisNow.UTC().UnixMilli()
	if err := b.writeRDB.ZRemRangeByScore(ctx, revocationNodesKey, "-inf", fmt.Sprintf("%d", nowMillis)).Err(); err != nil {
		return nil, err
	}
	nodes, err := b.writeRDB.ZRangeByScore(ctx, revocationNodesKey, &redis.ZRangeBy{
		Min: fmt.Sprintf("%d", nowMillis+1), Max: "+inf",
	}).Result()
	if err != nil {
		return nil, err
	}
	return normalizeParticipantIDs(nodes)
}

func (b *RedisConnectionCloseBroker) Dispatch(ctx context.Context, command tagaccess.ConnectionCloseCommand, participants []string) error {
	if b == nil || b.writeRDB == nil {
		return errors.New("realtime revocation broker unavailable")
	}
	participants, err := normalizeParticipantIDs(participants)
	if err != nil {
		return err
	}
	expected := make(map[string]struct{}, len(participants))
	for _, nodeID := range participants {
		expected[nodeID] = struct{}{}
	}
	payload, err := marshalConnectionCloseCommand(command)
	if err != nil {
		return err
	}
	commandID := connectionCloseCommandID(command)
	commandMAC := connectionCloseCommandMAC(b.authKey, commandID, payload)
	if err := b.writeRDB.XAdd(ctx, &redis.XAddArgs{
		Stream: revocationCommandStream, MaxLen: revocationCommandStreamMaxLen, Approx: true,
		Values: []any{"command_id", commandID, "payload_json", string(payload), "command_mac", commandMAC},
	}).Err(); err != nil {
		return err
	}

	poll := time.NewTicker(20 * time.Millisecond)
	defer poll.Stop()
	for {
		acks, err := b.writeRDB.HGetAll(ctx, revocationAckKey(commandID)).Result()
		if err != nil {
			return err
		}
		complete := true
		for nodeID := range expected {
			if !hmac.Equal([]byte(acks[nodeID]), []byte(connectionCloseAckValue(b.authKey, commandID, nodeID))) {
				complete = false
				break
			}
		}
		if complete {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-poll.C:
		}
	}
}

func (b *RedisConnectionCloseBroker) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(revocationHeartbeatPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := b.heartbeatOnce(ctx); err != nil {
				if b.leaseFailed.Load() {
					if b.hub != nil {
						b.hub.closeAllForBrokerShutdown()
					}
					return
				}
				slog.Warn("realtime revocation heartbeat failed; local lease remains fail-closed", "instance_id", b.instanceID, "error", err)
			}
		}
	}
}

func (b *RedisConnectionCloseBroker) heartbeatOnce(ctx context.Context) error {
	if b.leaseFailed.Load() {
		return errors.New("realtime revocation lease permanently fenced")
	}
	if deadline := b.leaseDeadline.Load(); deadline != nil && !time.Now().Before(*deadline) {
		b.leaseFailed.Store(true)
		return errors.New("realtime revocation lease expired")
	}
	// Fence locally before Redis can expire this boot. The safety margin is at
	// least the maximum heartbeat command duration, so a successful but delayed
	// renewal cannot leave the process serving past its advertised lease.
	localDeadline := time.Now().Add(revocationNodeTTL - 3*revocationHeartbeatPeriod)
	heartbeatCtx, cancel := context.WithTimeout(ctx, revocationHeartbeatPeriod)
	defer cancel()
	redisNow, err := b.writeRDB.Time(heartbeatCtx).Result()
	if err != nil {
		return err
	}
	if err := b.writeRDB.ZAdd(heartbeatCtx, revocationNodesKey, redis.Z{
		Score: float64(redisNow.UTC().Add(revocationNodeTTL).UnixMilli()), Member: b.instanceID,
	}).Err(); err != nil {
		return err
	}
	b.leaseDeadline.Store(&localDeadline)
	return nil
}

func (b *RedisConnectionCloseBroker) readLoop(ctx context.Context) {
	for ctx.Err() == nil {
		readCtx, cancel := context.WithTimeout(ctx, revocationReadBlock+100*time.Millisecond)
		streams, err := b.readRDB.XReadGroup(readCtx, &redis.XReadGroupArgs{
			Group: b.consumerGroup(), Consumer: b.instanceID,
			Streams: []string{revocationCommandStream, ">"}, Count: 32, Block: revocationReadBlock, NoAck: true,
		}).Result()
		cancel()
		if errors.Is(err, redis.Nil) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			continue
		}
		if err != nil {
			if b.failClosedOnReadError(err) {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(50 * time.Millisecond):
			}
			continue
		}
		for _, stream := range streams {
			for _, message := range stream.Messages {
				b.handleCommand(ctx, message)
			}
		}
	}
}

// failClosedOnReadError handles a reachable Redis that has lost the stream
// group (for example after a non-durable broker restart). Recreating at "$"
// could skip a close published during the reset, so this boot permanently
// self-fences and requires a process restart instead.
func (b *RedisConnectionCloseBroker) failClosedOnReadError(err error) bool {
	if b == nil || err == nil || !strings.Contains(err.Error(), "NOGROUP") {
		return false
	}
	if b.leaseFailed.CompareAndSwap(false, true) && b.hub != nil {
		b.hub.closeAllForBrokerShutdown()
	}
	return true
}

func (b *RedisConnectionCloseBroker) handleCommand(ctx context.Context, message redis.XMessage) {
	commandID := redisString(message.Values["command_id"])
	payload := redisString(message.Values["payload_json"])
	commandMAC := redisString(message.Values["command_mac"])
	if commandID == "" || payload == "" || !hmac.Equal([]byte(commandMAC), []byte(connectionCloseCommandMAC(b.authKey, commandID, []byte(payload)))) {
		return
	}
	var command tagaccess.ConnectionCloseCommand
	if err := json.Unmarshal([]byte(payload), &command); err != nil || connectionCloseCommandID(command) != commandID {
		return
	}
	if err := b.hub.ApplyConnectionClose(ctx, command); err != nil {
		return
	}
	ackCtx, cancel := context.WithTimeout(ctx, revocationHeartbeatPeriod)
	defer cancel()
	pipe := b.writeRDB.Pipeline()
	pipe.HSet(ackCtx, revocationAckKey(commandID), b.instanceID, connectionCloseAckValue(b.authKey, commandID, b.instanceID))
	pipe.Expire(ackCtx, revocationAckKey(commandID), revocationAckTTL)
	if _, err := pipe.Exec(ackCtx); err != nil {
		slog.Error("realtime revocation acknowledgement failed", "command_id", commandID, "instance_id", b.instanceID, "error", err)
	}
}

func marshalConnectionCloseCommand(command tagaccess.ConnectionCloseCommand) ([]byte, error) {
	if command.DeliveryID == "" || command.TargetDigest == "" || len(command.Targets) == 0 {
		return nil, errors.New("invalid connection-close command")
	}
	return json.Marshal(command)
}

func connectionCloseCommandID(command tagaccess.ConnectionCloseCommand) string {
	digest := sha256.Sum256([]byte(command.DeliveryID + "\x00" + command.TargetDigest))
	return hex.EncodeToString(digest[:])
}

func connectionCloseCommandMAC(key []byte, commandID string, payload []byte) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("tag-ws-close-command\x00"))
	_, _ = mac.Write([]byte(commandID))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

func connectionCloseAckValue(key []byte, commandID, instanceID string) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("tag-ws-close-ack\x00"))
	_, _ = mac.Write([]byte(commandID))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(instanceID))
	return "closed:" + hex.EncodeToString(mac.Sum(nil))
}

var _ ConnectionCloseBroker = (*RedisConnectionCloseBroker)(nil)
var _ connectionCloseLease = (*RedisConnectionCloseBroker)(nil)
