package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

const openclawNotificationTimeout = 30 * time.Second

var runOpenclawMessageCommand = func(ctx context.Context, payload protocol.NotificationDeliverPayload, cwd string) ([]byte, error) {
	channel := strings.TrimSpace(payload.OpenClawChannel)
	if channel == "" {
		channel = strings.TrimSpace(payload.Channel)
	}
	if channel == "" || channel == "openclaw_weixin" {
		channel = "openclaw-weixin"
	}
	cmd := exec.CommandContext(ctx, "openclaw", "message", "send",
		"-t", payload.WechatID,
		"-m", payload.Content,
		"--channel", channel,
	)
	cmd.Dir = cwd
	return cmd.CombinedOutput()
}

// handleNotificationDeliver processes a notification:deliver WS message by
// executing `openclaw message send` to deliver the notification via WeChat.
func (d *Daemon) handleNotificationDeliver(raw json.RawMessage, writes chan<- []byte) {
	var payload protocol.NotificationDeliverPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		d.logger.Warn("notification:deliver: invalid payload", "error", err)
		return
	}

	if payload.WechatID == "" || payload.Content == "" {
		d.logger.Debug("notification:deliver: missing wechat_id or content", "delivery_id", payload.DeliveryID)
		d.sendNotificationDeliveryResult(writes, payload, false, "missing wechat_id or content", "")
		return
	}

	cwd, err := resolveOpenclawNotificationCWD()
	if err != nil {
		d.logger.Warn("notification:deliver: resolve cwd failed", "delivery_id", payload.DeliveryID, "error", err)
		d.sendNotificationDeliveryResult(writes, payload, false, err.Error(), "")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), openclawNotificationTimeout)
	defer cancel()
	output, err := runOpenclawMessageCommand(ctx, payload, cwd)
	if err != nil {
		d.logger.Warn("notification:deliver: openclaw message send failed",
			"delivery_id", payload.DeliveryID,
			"wechat_id", payload.WechatID,
			"cwd", cwd,
			"error", err,
			"output", string(output),
		)
		d.sendNotificationDeliveryResult(writes, payload, false, err.Error(), string(output))
		return
	}

	d.logger.Info("notification:deliver: openclaw_weixin message sent",
		"delivery_id", payload.DeliveryID,
		"wechat_id", payload.WechatID,
		"cwd", cwd,
		"title", payload.Title,
	)
	d.sendNotificationDeliveryResult(writes, payload, true, "", string(output))
}

func (d *Daemon) sendNotificationDeliveryResult(writes chan<- []byte, payload protocol.NotificationDeliverPayload, success bool, errText, output string) {
	if strings.TrimSpace(payload.DeliveryID) == "" || writes == nil {
		return
	}
	result := protocol.NotificationDeliveryResultPayload{
		DeliveryID: payload.DeliveryID,
		Channel:    "openclaw_weixin",
		Success:    success,
		Error:      truncateNotificationDeliveryResultField(errText),
		Output:     truncateNotificationDeliveryResultField(output),
	}
	frame, err := json.Marshal(protocol.Message{
		Type:    protocol.EventNotificationDeliveryResult,
		Payload: marshalRaw(result),
	})
	if err != nil {
		d.logger.Debug("notification:deliver: result marshal failed", "delivery_id", payload.DeliveryID, "error", err)
		return
	}
	select {
	case writes <- frame:
	default:
		d.logger.Warn("notification:deliver: result dropped, writer backlog full", "delivery_id", payload.DeliveryID)
	}
}

func resolveOpenclawNotificationCWD() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("OPENCLAW_WORKSPACE")); dir != "" {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir, nil
		}
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		workspace := filepath.Join(home, ".openclaw", "workspace")
		if err := os.MkdirAll(workspace, 0o755); err == nil {
			if info, statErr := os.Stat(workspace); statErr == nil && info.IsDir() {
				return workspace, nil
			}
		}
		if info, statErr := os.Stat(home); statErr == nil && info.IsDir() {
			return home, nil
		}
	}
	tmp := os.TempDir()
	if info, err := os.Stat(tmp); err == nil && info.IsDir() {
		return tmp, nil
	}
	return "", errors.New("no stable cwd available for openclaw notification")
}

func truncateNotificationDeliveryResultField(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 500 {
		return value
	}
	return value[:500]
}
