// Package email implements the email delivery channel for inbox notifications.
//
// The Notifier subscribes to EventInboxNew on the event bus and, for each new
// inbox row, looks up the recipient's email + preferences and dispatches via
// the supplied EmailSender. The channel is strictly downstream of the inbox:
// if no inbox row is created (e.g. user muted the source group), no email
// fires. The Notifier itself only consults the email_notifications
// preference key.
package email

import "github.com/jackc/pgx/v5/pgtype"

// EmailSender abstracts the transport so tests can supply a fake. Production
// uses *service.EmailService.
type EmailSender interface {
	SendNotification(to, subject, textBody, htmlBody string) error
}

// InboxNotificationPayload is what we extract from an EventInboxNew event's
// payload map. It's intentionally a flat struct (not the inbox response type
// directly) to keep the email package independent of handler types.
type InboxNotificationPayload struct {
	RecipientID   pgtype.UUID
	RecipientType string // "member" today; we ignore non-member rows
	WorkspaceID   pgtype.UUID
	IssueID       pgtype.UUID
	IssueTitle    string
	NotifType     string // "issue_assigned", "mentioned", etc.
	IssueStatus   string
}
