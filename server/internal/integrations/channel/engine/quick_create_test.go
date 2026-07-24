package engine

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// quickCreatePrompt builds the quick-create prompt from the turn's OWN content:
// the composed body with inline image markdown kept in place and the /issue
// command token removed. A bare /issue with no content of its own yields "" (the
// caller shows the usage hint); an image is never dropped and never dragged in
// from a previous turn.
func TestQuickCreatePrompt(t *testing.T) {
	staged1 := []StagedMedia{{Filename: "image-1.png", URL: "https://cdn/x.png"}}
	staged2 := []StagedMedia{
		{Filename: "image-1.png", URL: "https://cdn/x.png"},
		{Filename: "image-2.png", URL: "https://cdn/y.png"},
	}

	cases := []struct {
		name   string
		msg    channel.InboundMessage
		staged []StagedMedia
		want   string
	}{
		{
			name: "plain text title",
			msg:  channel.InboundMessage{Text: "/issue fix login"},
			want: "fix login",
		},
		{
			name: "plain text title and description",
			msg:  channel.InboundMessage{Text: "/issue fix login\nsteps to repro"},
			want: "fix login\nsteps to repro",
		},
		{
			name: "bare issue yields empty prompt",
			msg:  channel.InboundMessage{Text: "/issue"},
			want: "",
		},
		{
			name:   "issue carrying only an image keeps the image inline",
			msg:    channel.InboundMessage{Text: "/issue", Segments: []channel.Segment{{Text: "/issue", MediaIdx: -1}, {MediaIdx: 0}}},
			staged: staged1,
			want:   "![image-1.png](https://cdn/x.png)",
		},
		{
			name: "text and image interleaving is preserved",
			msg: channel.InboundMessage{
				Text: "/issue before  after",
				Segments: []channel.Segment{
					{Text: "/issue before ", MediaIdx: -1},
					{MediaIdx: 0},
					{Text: " after ", MediaIdx: -1},
					{MediaIdx: 1},
				},
			},
			staged: staged2,
			want:   "before ![image-1.png](https://cdn/x.png) after ![image-2.png](https://cdn/y.png)",
		},
		{
			name:   "image-first bare issue keeps the image",
			msg:    channel.InboundMessage{Text: "/issue", Segments: []channel.Segment{{MediaIdx: 0}, {Text: "/issue", MediaIdx: -1}}},
			staged: staged1,
			want:   "![image-1.png](https://cdn/x.png)",
		},
		{
			name: "issuetracker is not the command token",
			msg:  channel.InboundMessage{Text: "/issuetracker foo"},
			want: "/issuetracker foo",
		},
		{
			name: "newline right after the command token is a boundary",
			msg:  channel.InboundMessage{Text: "/issue\nsteps to repro"},
			want: "steps to repro",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := quickCreatePrompt(tc.msg, tc.staged); got != tc.want {
				t.Errorf("quickCreatePrompt = %q, want %q", got, tc.want)
			}
		})
	}
}
