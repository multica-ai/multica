// Package collab connects the API to the live co-editing service
// (apps/collab): it mints the tickets editors connect with, sends server-side
// writes through the service so they merge into what people are typing, and
// proxies editor WebSockets to it.
package collab

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"time"
)

// Actor is who a ticket or a write speaks for.
type Actor struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Ticket is what the API vouches for when it lets an editor connect to a
// document. It is verified by apps/collab/src/ticket.ts.
type Ticket struct {
	Document    string
	WorkspaceID string
	Actor       Actor
	ReadOnly    bool
	ExpiresAt   time.Time
}

type ticketPayload struct {
	D   string `json:"d"`
	W   string `json:"w"`
	T   string `json:"t"`
	I   string `json:"i"`
	N   string `json:"n"`
	RO  bool   `json:"ro"`
	Exp int64  `json:"exp"`
}

// SignTicket returns `<base64url JSON payload>.<base64url HMAC-SHA256>`.
func SignTicket(secret string, t Ticket) string {
	raw, _ := json.Marshal(ticketPayload{
		D: t.Document, W: t.WorkspaceID, T: t.Actor.Type, I: t.Actor.ID, N: t.Actor.Name,
		RO: t.ReadOnly, Exp: t.ExpiresAt.Unix(),
	})
	payload := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// DocumentName is the collaboration service's name for a doc.
func DocumentName(docID string) string {
	return "doc:" + docID
}
