package engine

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

func TestComposeBody(t *testing.T) {
	staged := []StagedMedia{
		{Filename: "image-1.png", URL: "https://files.test/i1.png"},
		{Filename: "image-2.jpg", URL: "https://files.test/i2.jpg"},
	}

	tests := []struct {
		name string
		msg  channel.InboundMessage
		want string
	}{
		{
			name: "no segments returns text unchanged",
			msg:  channel.InboundMessage{Text: "hello there"},
			want: "hello there",
		},
		{
			name: "text and media interleaved",
			msg: channel.InboundMessage{Segments: []channel.Segment{
				{Text: "look ", MediaIdx: -1},
				{MediaIdx: 0},
				{Text: " now", MediaIdx: -1},
			}},
			want: "look ![image-1.png](https://files.test/i1.png) now",
		},
		{
			name: "media only",
			msg:  channel.InboundMessage{Segments: []channel.Segment{{MediaIdx: 0}}},
			want: "![image-1.png](https://files.test/i1.png)",
		},
		{
			name: "two images back to back",
			msg: channel.InboundMessage{Segments: []channel.Segment{
				{MediaIdx: 0},
				{MediaIdx: 1},
			}},
			want: "![image-1.png](https://files.test/i1.png) ![image-2.jpg](https://files.test/i2.jpg)",
		},
		{
			name: "media index out of range degrades to bare marker",
			msg:  channel.InboundMessage{Segments: []channel.Segment{{MediaIdx: 5}}},
			want: "[image]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ComposeBody(tt.msg, staged); got != tt.want {
				t.Errorf("ComposeBody() = %q, want %q", got, tt.want)
			}
		})
	}
}
