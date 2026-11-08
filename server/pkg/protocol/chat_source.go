package protocol

import (
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

// ChatSource is optional, server-resolved provenance for the current Slack
// task. It contains no installation credentials or binding configuration.
type ChatSource struct {
	ChannelID   string `json:"channel_id,omitempty"`
	ChannelName string `json:"channel_name,omitempty"`
	ThreadID    string `json:"thread_id,omitempty"`
	Permalink   string `json:"permalink,omitempty"`
}

var slackChannelID = regexp.MustCompile(`^[CGD][A-Z0-9]+$`)
var slackMessageTS = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)
var slackPermalinkPath = regexp.MustCompile(`^/archives/([CGD][A-Z0-9]+)/p[0-9]+$`)

// SafeSlackSource validates provider data at both ends of the claim boundary.
// Names remain untrusted text; callers must encode them when rendering prompts.
func SafeSlackSource(source *ChatSource) *ChatSource {
	if source == nil || len(source.ChannelID) > 64 || !slackChannelID.MatchString(source.ChannelID) {
		return nil
	}
	out := *source
	if len(out.ThreadID) > 64 || !slackMessageTS.MatchString(out.ThreadID) {
		out.ThreadID = ""
	}
	if len([]rune(out.ChannelName)) > 80 || strings.IndexFunc(out.ChannelName, unicode.IsControl) >= 0 {
		out.ChannelName = ""
	}
	u, err := url.Parse(out.Permalink)
	if err != nil || len(out.Permalink) > 2048 || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.Fragment != "" || u.RawPath != "" {
		out.Permalink = ""
		return &out
	}
	host := strings.ToLower(u.Hostname())
	path := slackPermalinkPath.FindStringSubmatch(u.Path)
	if !(strings.HasSuffix(host, ".slack.com") || strings.HasSuffix(host, ".slack-gov.com")) || len(path) != 2 || path[1] != out.ChannelID {
		out.Permalink = ""
		return &out
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		out.Permalink = ""
		return &out
	}
	for key, values := range query {
		if len(values) != 1 || (key != "thread_ts" && key != "cid") || (key == "thread_ts" && !slackMessageTS.MatchString(values[0])) || (key == "cid" && values[0] != out.ChannelID) {
			out.Permalink = ""
			break
		}
	}
	return &out
}
