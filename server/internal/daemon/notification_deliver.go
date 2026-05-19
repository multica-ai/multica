package daemon

import (
	"encoding/json"
	"os/exec"
)

// notificationDeliverPayload matches the server's openclawWeixinDaemonPayload.
type notificationDeliverPayload struct {
	Type     string `json:"type"`
	WechatID string `json:"wechat_id"`
	Channel  string `json:"channel"`
	Content  string `json:"content"`
	Title    string `json:"title"`
	Link     string `json:"link"`
}

// handleNotificationDeliver processes a notification:deliver WS message by
// executing `openclaw message send` to deliver the notification via WeChat.
func (d *Daemon) handleNotificationDeliver(raw json.RawMessage) {
	var payload notificationDeliverPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		d.logger.Warn("notification:deliver: invalid payload", "error", err)
		return
	}

	if payload.WechatID == "" || payload.Content == "" {
		d.logger.Debug("notification:deliver: missing wechat_id or content")
		return
	}

	channel := payload.Channel
	if channel == "" {
		channel = "openclaw-weixin"
	}

	cmd := exec.Command("openclaw", "message", "send",
		"-t", payload.WechatID,
		"-m", payload.Content,
		"--channel", channel,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		d.logger.Warn("notification:deliver: openclaw message send failed",
			"wechat_id", payload.WechatID,
			"error", err,
			"output", string(output),
		)
		return
	}

	d.logger.Info("notification:deliver: openclaw_weixin message sent",
		"wechat_id", payload.WechatID,
		"title", payload.Title,
	)
}
