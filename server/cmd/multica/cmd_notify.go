package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var notifyCmd = &cobra.Command{
	Use:   "notify",
	Short: "Manage notification settings",
}

var notifyBindWechatCmd = &cobra.Command{
	Use:   "bind-wechat",
	Short: "Bind OpenClaw WeChat notifications",
	Args:  exactArgs(0),
	RunE:  runNotifyBindWechat,
}

type notifyBindWechatRequest struct {
	WechatID string `json:"wechat_id"`
	Channel  string `json:"channel"`
}

type notifyBindingResponse struct {
	ID             string         `json:"id"`
	Provider       string         `json:"provider"`
	ExternalUserID string         `json:"external_user_id"`
	DisplayName    *string        `json:"display_name"`
	Status         string         `json:"status"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	CreatedAt      string         `json:"created_at"`
	UpdatedAt      string         `json:"updated_at"`
}

type notifyBindWechatResult struct {
	Success  bool                   `json:"success"`
	WechatID string                 `json:"wechat_id"`
	Channel  string                 `json:"channel"`
	Binding  *notifyBindingResponse `json:"binding,omitempty"`
}

func init() {
	notifyCmd.AddCommand(notifyBindWechatCmd)

	notifyBindWechatCmd.Flags().String("wechat-id", "", "OpenClaw WeChat user ID to bind")
	notifyBindWechatCmd.Flags().String("channel", "openclaw-weixin", "OpenClaw notification channel")
	notifyBindWechatCmd.Flags().String("output", "table", "Output format: table or json")
}

func runNotifyBindWechat(cmd *cobra.Command, _ []string) error {
	wechatID, _ := cmd.Flags().GetString("wechat-id")
	wechatID = strings.TrimSpace(wechatID)
	if wechatID == "" {
		return fmt.Errorf("--wechat-id is required")
	}

	channel, _ := cmd.Flags().GetString("channel")
	channel = strings.TrimSpace(channel)
	if channel == "" {
		channel = "openclaw-weixin"
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if strings.TrimSpace(client.Token) == "" {
		return fmt.Errorf("not authenticated: run 'multica auth login' first")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var binding notifyBindingResponse
	if err := client.PutJSON(ctx, "/api/me/notification-bindings/openclaw-weixin", notifyBindWechatRequest{
		WechatID: wechatID,
		Channel:  channel,
	}, &binding); err != nil {
		return fmt.Errorf("bind wechat notification: %w", err)
	}

	result := notifyBindWechatResult{
		Success:  true,
		WechatID: binding.ExternalUserID,
		Channel:  channel,
		Binding:  &binding,
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(cmd.OutOrStdout(), result)
	}
	if output != "table" {
		return fmt.Errorf("unsupported output format %q (expected table or json)", output)
	}

	cli.PrintTable(cmd.OutOrStdout(), []string{"STATUS", "WECHAT ID", "CHANNEL", "BINDING ID"}, [][]string{{
		binding.Status,
		binding.ExternalUserID,
		channel,
		binding.ID,
	}})
	return nil
}
