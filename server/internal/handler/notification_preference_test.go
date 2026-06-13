package handler

import "testing"

func TestValidNotifGroups_IncludesEmailNotifications(t *testing.T) {
	if !validNotifGroups["email_notifications"] {
		t.Error("email_notifications must be a valid notification group")
	}
}
