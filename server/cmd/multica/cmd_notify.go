package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
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

var notifyDebugDeliveriesCmd = &cobra.Command{
	Use:   "debug-deliveries",
	Short: "List notification event/delivery debug rows (workspace admin only)",
	Args:  exactArgs(0),
	RunE:  runNotifyDebugDeliveries,
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

type notifyDebugEvent struct {
	ID              string  `json:"id"`
	RecipientUserID string  `json:"recipient_user_id"`
	Type            string  `json:"type"`
	IssueID         *string `json:"issue_id"`
	CommentID       *string `json:"comment_id"`
	ActorType       *string `json:"actor_type"`
	ActorID         *string `json:"actor_id"`
	CreatedAt       string  `json:"created_at"`
}

type notifyDebugDelivery struct {
	ID              string          `json:"id"`
	Channel         string          `json:"channel"`
	Status          string          `json:"status"`
	AttemptCount    int32           `json:"attempt_count"`
	LastError       *string         `json:"last_error"`
	PayloadSnapshot json.RawMessage `json:"payload_snapshot"`
	SentAt          *string         `json:"sent_at"`
	CreatedAt       string          `json:"created_at"`
	UpdatedAt       string          `json:"updated_at"`
}

type notifyDebugRow struct {
	NotificationEvent notifyDebugEvent     `json:"notification_event"`
	Delivery          *notifyDebugDelivery `json:"delivery"`
}

type notifyDebugDeliveriesResult struct {
	Rows  []notifyDebugRow `json:"rows"`
	Total int              `json:"total"`
}

func init() {
	notifyCmd.AddCommand(notifyBindWechatCmd)
	notifyCmd.AddCommand(notifyDebugDeliveriesCmd)

	notifyBindWechatCmd.Flags().String("wechat-id", "", "OpenClaw WeChat user ID to bind")
	notifyBindWechatCmd.Flags().String("channel", "openclaw-weixin", "OpenClaw notification channel")
	notifyBindWechatCmd.Flags().String("output", "table", "Output format: table or json")

	notifyDebugDeliveriesCmd.Flags().String("workspace-id", "", "Workspace UUID (defaults to configured workspace)")
	notifyDebugDeliveriesCmd.Flags().String("issue-id", "", "Filter by issue UUID")
	notifyDebugDeliveriesCmd.Flags().String("recipient-id", "", "Filter by recipient user UUID")
	notifyDebugDeliveriesCmd.Flags().String("comment-id", "", "Filter by comment UUID")
	notifyDebugDeliveriesCmd.Flags().String("event-type", "", "Filter by notification event type")
	notifyDebugDeliveriesCmd.Flags().String("channel", "", "Filter delivery channel; events without a delivery for this channel still appear")
	notifyDebugDeliveriesCmd.Flags().Int("limit", 100, "Maximum rows to return (1-200)")
	notifyDebugDeliveriesCmd.Flags().String("output", "table", "Output format: table or json")
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

func runNotifyDebugDeliveries(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if strings.TrimSpace(client.Token) == "" {
		return fmt.Errorf("not authenticated: run 'multica auth login' first")
	}

	workspaceID, _ := cmd.Flags().GetString("workspace-id")
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		workspaceID = strings.TrimSpace(client.WorkspaceID)
	}
	if workspaceID == "" {
		return fmt.Errorf("--workspace-id is required when no default workspace is configured")
	}

	limit, _ := cmd.Flags().GetInt("limit")
	if limit < 1 || limit > 200 {
		return fmt.Errorf("--limit must be between 1 and 200")
	}

	params := url.Values{}
	params.Set("limit", fmt.Sprintf("%d", limit))
	for _, flag := range []struct {
		name  string
		param string
	}{
		{name: "issue-id", param: "issue_id"},
		{name: "recipient-id", param: "recipient_id"},
		{name: "comment-id", param: "comment_id"},
		{name: "event-type", param: "event_type"},
		{name: "channel", param: "channel"},
	} {
		value, _ := cmd.Flags().GetString(flag.name)
		if value = strings.TrimSpace(value); value != "" {
			params.Set(flag.param, value)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	path := "/api/workspaces/" + url.PathEscape(workspaceID) + "/notification-debug/deliveries"
	if encoded := params.Encode(); encoded != "" {
		path += "?" + encoded
	}

	var result notifyDebugDeliveriesResult
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("list notification debug deliveries: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(cmd.OutOrStdout(), result)
	}
	if output != "table" {
		return fmt.Errorf("unsupported output format %q (expected table or json)", output)
	}

	rows := make([][]string, 0, len(result.Rows))
	for _, row := range result.Rows {
		event := row.NotificationEvent
		channel := "(missing)"
		status := "(missing)"
		attempts := ""
		lastError := ""
		if row.Delivery != nil {
			channel = row.Delivery.Channel
			status = row.Delivery.Status
			attempts = fmt.Sprintf("%d", row.Delivery.AttemptCount)
			if row.Delivery.LastError != nil {
				lastError = *row.Delivery.LastError
			}
		}
		commentID := ""
		if event.CommentID != nil {
			commentID = *event.CommentID
		}
		rows = append(rows, []string{
			event.ID,
			event.Type,
			commentID,
			channel,
			status,
			attempts,
			lastError,
		})
	}
	cli.PrintTable(cmd.OutOrStdout(), []string{"EVENT ID", "TYPE", "COMMENT ID", "CHANNEL", "STATUS", "ATTEMPTS", "LAST ERROR"}, rows)
	return nil
}
