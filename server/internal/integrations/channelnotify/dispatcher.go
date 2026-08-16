package channelnotify

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	defaultQueueSize     = 256
	defaultWorkers       = 8
	defaultDeliveryLimit = 15 * time.Second
)

type notificationResolver interface {
	Resolve(context.Context, Notification, channel.Type) (Target, bool, error)
}

type Config struct {
	Enabled       []channel.Type
	QueueSize     int
	Workers       int
	DeliveryLimit time.Duration
	Logger        *slog.Logger
}

// Dispatcher keeps database and Channel API work off the synchronous event
// bus. Its queue is intentionally bounded and best-effort; the durable Inbox
// item remains authoritative when a delivery is dropped or fails.
type Dispatcher struct {
	resolver      notificationResolver
	registry      *Registry
	enabled       []channel.Type
	deliveryLimit time.Duration
	logger        *slog.Logger

	admissionMu sync.RWMutex
	accepting   bool
	queue       chan Notification
	closeOnce   sync.Once
	workers     sync.WaitGroup
	done        chan struct{}
}

func NewDispatcher(resolver notificationResolver, registry *Registry, config Config) *Dispatcher {
	if config.QueueSize <= 0 {
		config.QueueSize = defaultQueueSize
	}
	if config.Workers <= 0 {
		config.Workers = defaultWorkers
	}
	if config.DeliveryLimit <= 0 {
		config.DeliveryLimit = defaultDeliveryLimit
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	if registry == nil {
		registry = NewRegistry()
	}

	dispatcher := &Dispatcher{
		resolver:      resolver,
		registry:      registry,
		enabled:       uniqueChannelTypes(config.Enabled),
		deliveryLimit: config.DeliveryLimit,
		logger:        config.Logger,
		accepting:     true,
		queue:         make(chan Notification, config.QueueSize),
		done:          make(chan struct{}),
	}
	dispatcher.workers.Add(config.Workers)
	for i := 0; i < config.Workers; i++ {
		go dispatcher.runWorker()
	}
	go func() {
		dispatcher.workers.Wait()
		close(dispatcher.done)
	}()
	return dispatcher
}

func (d *Dispatcher) Register(bus *events.Bus) {
	if d == nil || bus == nil {
		return
	}
	bus.Subscribe(protocol.EventInboxNew, d.handleEvent)
}

func (d *Dispatcher) handleEvent(event events.Event) {
	notification, ok := ParseNotification(event)
	if !ok {
		d.logger.Debug("skipping ineligible Inbox Channel notification", "event_type", event.Type)
		return
	}

	d.admissionMu.RLock()
	defer d.admissionMu.RUnlock()
	if !d.accepting {
		return
	}
	select {
	case d.queue <- notification:
	default:
		d.logger.Warn("Inbox Channel notification queue is full; dropping delivery",
			"inbox_item_id", logUUID(notification.InboxItemID.Bytes),
			"issue_id", logUUID(notification.IssueID.Bytes))
	}
}

func (d *Dispatcher) runWorker() {
	defer d.workers.Done()
	for notification := range d.queue {
		d.deliver(notification)
	}
}

func (d *Dispatcher) deliver(notification Notification) {
	for _, channelType := range d.enabled {
		sender, ok := d.registry.Lookup(channelType)
		if !ok {
			continue
		}
		if d.resolver == nil {
			d.logger.Warn("Inbox Channel notification resolver is unavailable", "channel_type", channelType)
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), d.deliveryLimit)
		target, found, err := d.resolver.Resolve(ctx, notification, channelType)
		if err != nil {
			cancel()
			d.logger.Warn("failed to resolve Inbox Channel notification target",
				"channel_type", channelType,
				"inbox_item_id", logUUID(notification.InboxItemID.Bytes),
				"issue_id", logUUID(notification.IssueID.Bytes),
				"error", err)
			continue
		}
		if !found {
			cancel()
			d.logger.Debug("no eligible Inbox Channel notification target",
				"channel_type", channelType,
				"inbox_item_id", logUUID(notification.InboxItemID.Bytes),
				"issue_id", logUUID(notification.IssueID.Bytes),
				"recipient_id", logUUID(notification.RecipientID.Bytes))
			continue
		}
		if err := sender.SendInbox(ctx, target, notification); err != nil {
			d.logger.Warn("failed to send Inbox Channel notification",
				"channel_type", channelType,
				"inbox_item_id", logUUID(notification.InboxItemID.Bytes),
				"issue_id", logUUID(notification.IssueID.Bytes),
				"installation_id", logUUID(target.InstallationID.Bytes),
				"error", err)
		}
		cancel()
	}
}

func (d *Dispatcher) Close(ctx context.Context) error {
	if d == nil {
		return nil
	}
	d.closeOnce.Do(func() {
		d.admissionMu.Lock()
		d.accepting = false
		close(d.queue)
		d.admissionMu.Unlock()
	})
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-d.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func uniqueChannelTypes(values []channel.Type) []channel.Type {
	seen := make(map[channel.Type]struct{}, len(values))
	result := make([]channel.Type, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func logUUID(value [16]byte) string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16])
}
