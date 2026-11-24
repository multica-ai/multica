// Package lark adapts Feishu bot traffic to the shared channel engine.
// Installation and binding services handle encrypted credentials and user binding;
// ResolverSet supplies platform-specific identity, dedup, session, and reply logic.
// The hub owns leased WebSocket connections, and outbound patchers update cards.
//
// Issue creation uses service.IssueService.Create. Chat ingestion uses
// engine.ChatSession rather than HTTP handlers, whose single-creator checks do
// not represent group chats. Outbound card mappings live in their own table.
// Unbound or non-member senders are audited without storing message bodies;
// app_secret is encrypted before persistence.
package lark
