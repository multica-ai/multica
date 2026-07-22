package main

import "strings"

var cerebroAudienceSplitTypes = map[string]bool{
	"new_comment":              true,
	"status_changed":           true,
	"agent_comment_no_tag":     true,
	"agent_comment_member_tag": true,
	"agent_comment_agent_tag":  true,
}

func isCerebroAudienceSplitType(notifType string) bool {
	return cerebroAudienceSplitTypes[notifType]
}

func cerebroNotificationChoice(block map[string]any, key string) string {
	if value, ok := block[key].(string); ok {
		return value
	}
	base, audience, found := strings.Cut(key, ".")
	if !found || (audience != "assignee" && audience != "follower") || !isCerebroAudienceSplitType(base) {
		return ""
	}
	value, _ := block[base].(string)
	return value
}

func cerebroNotificationChannelDefault(defaults map[string]bool, channel, key string) bool {
	if value, ok := defaults[key]; ok {
		return value
	}

	base, audience, found := strings.Cut(key, ".")
	if !found || !isCerebroAudienceSplitType(base) {
		return false
	}
	if audience == "assignee" {
		return defaults[base]
	}
	if audience != "follower" {
		return false
	}

	switch channel {
	case channelInbox:
		return true
	case channelMobile, channelDesktop:
		return false
	default:
		return defaults[base]
	}
}
