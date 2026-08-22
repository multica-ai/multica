package lark

// inbox_digest.go — one growing card per issue instead of a card per event.
// A burst of agent activity on an issue (status flip, comment, another flip)
// used to land as three separate cards in as many seconds; the recipient
// reads that as noise, not as one story. The first event for an issue sends
// a card and remembers its message id; every further event inside the
// window patches that card, appending a block. Distinct issues keep
// distinct cards — merging across issues would tell the wrong story.

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

const (
	// inboxDigestWindow is how long after its last event a card keeps
	// absorbing new ones. Long enough to cover an agent working an issue,
	// short enough that tomorrow's activity starts a fresh card.
	inboxDigestWindow = 15 * time.Minute
	// inboxDigestMaxBlocks caps a single card's appended events; past it a
	// fresh card starts so one hyperactive issue cannot grow a monster.
	inboxDigestMaxBlocks = 12
)

// inboxDigest is the live card for one (recipient, issue).
type inboxDigest struct {
	mu        sync.Mutex
	messageID string
	header    string
	body      string
	blocks    []string
	link      string
	lastAt    time.Time
}

// digestKey scopes a digest to one recipient and one issue: the same
// event fans out to several bound members, each with their own card.
func digestKey(openID, issueID string) string { return openID + "|" + issueID }

// digestFor returns the digest entry for a key, creating it if needed, and
// lazily prunes stale entries so the map tracks live cards, not history.
func (p *Patcher) digestFor(key string, now time.Time) *inboxDigest {
	p.inboxMu.Lock()
	defer p.inboxMu.Unlock()
	if p.inboxDigests == nil {
		p.inboxDigests = make(map[string]*inboxDigest)
	}
	if len(p.inboxDigests) > 256 {
		for k, d := range p.inboxDigests {
			if now.Sub(d.lastAt) > inboxDigestWindow {
				delete(p.inboxDigests, k)
			}
		}
	}
	d, ok := p.inboxDigests[key]
	if !ok {
		d = &inboxDigest{}
		p.inboxDigests[key] = d
	}
	return d
}

// markdownCardJSON builds the same schema-2.0 envelope SendMarkdownCard
// builds internally, for the patch path, which takes raw card JSON.
func markdownCardJSON(markdown, summary string) (string, error) {
	cfg := map[string]any{"update_multi": true}
	if summary != "" {
		cfg["summary"] = map[string]any{"content": summary}
	}
	card := map[string]any{
		"schema": "2.0",
		"config": cfg,
		"body": map[string]any{
			"elements": []any{
				map[string]any{"tag": "markdown", "content": markdown},
			},
		},
	}
	b, err := json.Marshal(card)
	return string(b), err
}

// deliverDigested sends the event as a fresh card or folds it into the
// issue's live card. Reports whether anything reached Lark.
func (p *Patcher) deliverDigested(ctx context.Context, creds InstallationCredentials, openID string, issueID string, parts inboxCardParts) bool {
	log := p.cfg.Logger
	now := p.cfg.Now()

	// send posts a fresh card and, when d is non-nil, re-seeds that digest
	// entry. d is the caller's ALREADY-LOCKED entry — send must not lock it
	// again, which is why it takes the pointer instead of looking it up.
	send := func(d *inboxDigest) bool {
		msgID, err := p.client.SendMarkdownCard(ctx, SendMarkdownCardParams{
			InstallationID: creds,
			ChatID:         ChatID(openID),
			Markdown:       composeInboxCard(parts.header, parts.body, nil, parts.link),
			Summary:        parts.summary,
			ReceiveIDType:  ReceiveIDOpenID,
		})
		if err != nil {
			log.WarnContext(ctx, "lark inbox push: send failed", "error", err, "recipient_open_id", openID)
			return false
		}
		if d != nil && msgID != "" {
			d.messageID, d.header, d.body, d.link = msgID, parts.header, parts.body, parts.link
			d.blocks = nil
			d.lastAt = now
		}
		return true
	}

	// Chat-only notifications have no issue to group under.
	if issueID == "" {
		return send(nil)
	}

	d := p.digestFor(digestKey(openID, issueID), now)
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.messageID == "" || now.Sub(d.lastAt) > inboxDigestWindow || len(d.blocks) >= inboxDigestMaxBlocks {
		return send(d)
	}

	// Fold this event into the live card. The block repeats the label, not
	// the title — the card is already about this issue.
	block := "**" + parts.label + "** " + now.Format("15:04")
	if parts.body != "" {
		block += "\n\n" + parts.body
	}
	markdown := composeInboxCard(d.header, d.body, append(d.blocks, block), d.link)
	cardJSON, err := markdownCardJSON(markdown, parts.summary)
	if err != nil {
		log.WarnContext(ctx, "lark inbox push: encode digest card failed", "error", err)
		return send(d)
	}
	if err := p.client.PatchInteractiveCard(ctx, PatchCardParams{
		InstallationID:    creds,
		LarkCardMessageID: d.messageID,
		CardJSON:          cardJSON,
	}); err != nil {
		// A card Lark no longer lets us patch (recalled, expired) should
		// not swallow the event: fall back to a fresh card, which also
		// re-seeds the digest.
		log.WarnContext(ctx, "lark inbox push: digest patch failed; sending fresh card", "error", err)
		return send(d)
	}
	d.blocks = append(d.blocks, block)
	d.lastAt = now
	return true
}
