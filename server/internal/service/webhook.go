package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	webhookTimeout      = 10 * time.Second
	maxRetries          = 3
	maxResponseBodySize = 1024
)

// WebhookPayload is the standard payload POSTed to webhook endpoints.
type WebhookPayload struct {
	Event       string `json:"event"`
	WorkspaceID string `json:"workspace_id"`
	Timestamp   string `json:"timestamp"`
	Data        any    `json:"data"`
}

// WebhookService delivers outbound webhooks with HMAC-SHA256 signing and retry.
type WebhookService struct {
	Queries *db.Queries
	client  *http.Client
}

// NewWebhookService creates a webhook service with a pre-configured HTTP client.
func NewWebhookService(queries *db.Queries) *WebhookService {
	return &WebhookService{
		Queries: queries,
		client: &http.Client{
			Timeout: webhookTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// Sign computes the HMAC-SHA256 signature for a payload using the endpoint secret.
func Sign(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// Deliver sends a webhook payload to an endpoint and records the delivery.
// Runs asynchronously — returns immediately after creating the delivery record.
func (s *WebhookService) Deliver(ctx context.Context, endpoint db.WebhookEndpoint, eventType string, data any) {
	payload := WebhookPayload{
		Event:       eventType,
		WorkspaceID: util.UUIDToString(endpoint.WorkspaceID),
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		Data:        data,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		slog.Error("webhook: failed to marshal payload",
			"endpoint_id", util.UUIDToString(endpoint.ID),
			"event", eventType,
			"error", err)
		return
	}

	delivery, err := s.Queries.CreateWebhookDelivery(ctx, db.CreateWebhookDeliveryParams{
		EndpointID: endpoint.ID,
		EventType:  eventType,
		Payload:    body,
		Status:     "pending",
		Attempt:    1,
	})
	if err != nil {
		slog.Error("webhook: failed to create delivery record",
			"endpoint_id", util.UUIDToString(endpoint.ID),
			"error", err)
		return
	}

	go s.deliverWithRetry(endpoint, delivery.ID, body)
}

// DeliverToWorkspace finds all enabled endpoints in a workspace that subscribe
// to the given event type, and delivers the payload to each.
func (s *WebhookService) DeliverToWorkspace(ctx context.Context, workspaceID pgtype.UUID, eventType string, data any) {
	endpoints, err := s.Queries.ListEnabledWebhookEndpoints(ctx, workspaceID)
	if err != nil {
		slog.Error("webhook: failed to list endpoints",
			"workspace_id", util.UUIDToString(workspaceID),
			"error", err)
		return
	}

	for _, ep := range endpoints {
		if !endpointSubscribedTo(ep, eventType) {
			continue
		}
		s.Deliver(ctx, ep, eventType, data)
	}
}

func endpointSubscribedTo(ep db.WebhookEndpoint, eventType string) bool {
	// Empty event_types array means subscribe to all events.
	if len(ep.EventTypes) == 0 {
		return true
	}
	for _, et := range ep.EventTypes {
		if et == eventType || et == "*" {
			return true
		}
	}
	return false
}

func (s *WebhookService) deliverWithRetry(endpoint db.WebhookEndpoint, deliveryID pgtype.UUID, body []byte) {
	ctx := context.Background()
	signature := Sign(body, endpoint.Secret)

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			time.Sleep(time.Duration(1<<uint(attempt)) * time.Second)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.Url, bytes.NewReader(body))
		if err != nil {
			s.updateStatus(ctx, deliveryID, "failed", attempt, 0, "", err.Error())
			return
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "Multica-Webhooks/1.0")
		req.Header.Set("X-Webhook-Signature", signature)
		req.Header.Set("X-Webhook-Event", endpoint.EventTypes[0])
		req.Header.Set("X-Webhook-Delivery", util.UUIDToString(deliveryID))

		resp, err := s.client.Do(req)
		if err != nil {
			slog.Warn("webhook: request failed",
				"endpoint_id", util.UUIDToString(endpoint.ID),
				"attempt", attempt,
				"error", err)
			if attempt == maxRetries {
				s.updateStatus(ctx, deliveryID, "failed", attempt, 0, "", err.Error())
			}
			continue
		}

		respBody := readLimitedBody(resp.Body)
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			s.updateStatus(ctx, deliveryID, "delivered", attempt, resp.StatusCode, respBody, "")
			slog.Info("webhook: delivered",
				"endpoint_id", util.UUIDToString(endpoint.ID),
				"status", resp.StatusCode)
			return
		}

		slog.Warn("webhook: non-2xx response",
			"endpoint_id", util.UUIDToString(endpoint.ID),
			"status", resp.StatusCode,
			"attempt", attempt)

		if resp.StatusCode < 500 {
			s.updateStatus(ctx, deliveryID, "failed", attempt, resp.StatusCode, respBody,
				fmt.Sprintf("HTTP %d", resp.StatusCode))
			return
		}

		if attempt == maxRetries {
			s.updateStatus(ctx, deliveryID, "failed", attempt, resp.StatusCode, respBody,
				fmt.Sprintf("HTTP %d after %d attempts", resp.StatusCode, maxRetries))
		}
	}
}

func (s *WebhookService) updateStatus(ctx context.Context, deliveryID pgtype.UUID, status string, attempt, httpStatus int, respBody, errMsg string) {
	var httpStatusVal pgtype.Int4
	if httpStatus > 0 {
		httpStatusVal = pgtype.Int4{Int32: int32(httpStatus), Valid: true}
	}
	var respBodyVal pgtype.Text
	if respBody != "" {
		respBodyVal = pgtype.Text{String: respBody, Valid: true}
	}
	var errMsgVal pgtype.Text
	if errMsg != "" {
		errMsgVal = pgtype.Text{String: errMsg, Valid: true}
	}

	_, err := s.Queries.UpdateWebhookDeliveryStatus(ctx, db.UpdateWebhookDeliveryStatusParams{
		ID:           deliveryID,
		Status:       status,
		HttpStatus:   httpStatusVal,
		ResponseBody: respBodyVal,
		ErrorMessage: errMsgVal,
		Attempt:      int32(attempt),
	})
	if err != nil {
		slog.Error("webhook: failed to update delivery status",
			"delivery_id", util.UUIDToString(deliveryID),
			"status", status,
			"error", err)
	}
}

func readLimitedBody(r io.ReadCloser) string {
	buf := make([]byte, maxResponseBodySize)
	n, _ := io.ReadFull(r, buf)
	return string(buf[:n])
}
