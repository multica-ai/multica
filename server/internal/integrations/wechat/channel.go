package wechat

import (
	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// TypeWechat is the channel discriminator for the WeChat ClawBot (iLink)
// adapter. It is defined here (not in the channel core package) on purpose:
// registering a new platform must not require editing the core, so the Type
// value lives with its adapter — the same convention slack/ follows
// (slack/channel.go). The value "wechat" (not "wecom") reflects that this is the
// personal-WeChat ClawBot / iLink protocol, not the WeCom (企业微信) API.
const TypeWechat channel.Type = "wechat"

// originWechatChat is the issue.origin_type label stamped on issues created via
// the WeChat `/issue` command path. It MUST appear in the issue_origin_type_check
// CHECK constraint (migration 162) or IssueService.Create trips SQLSTATE 23514 —
// the same gap 131 fixed for Slack and 111 fixed for Lark.
const originWechatChat = "wechat_chat"

// maxMessageRunes caps a single outbound message body. WeChat's iLink
// sendmessage tolerates large bodies but we chunk below a conservative limit to
// stay well under any undocumented per-message cap; long agent replies are
// delivered as successive messages.
const maxMessageRunes = 2000
