package execenv

import "testing"

// ChannelCarriesFiles is the delivery half of the MUL-4899 channel policy. It
// decides whether an agent is told to run `multica attachment upload` or to
// describe its file in words, so a wrong answer is not cosmetic: a false
// positive has the agent write "see the attached chart" into a conversation
// that never receives one.
func TestChannelCarriesFiles(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		channelType string
		want        bool
	}{
		{
			// The adapter reads the bound attachment out of object storage and
			// sends it into the chat behind the answer.
			name:        "wecom delivers files",
			channelType: ChannelTypeWecom,
			want:        true,
		},
		{
			name:        "slack does not",
			channelType: ChannelTypeSlack,
			want:        false,
		},
		{
			name:        "feishu does not",
			channelType: ChannelTypeFeishu,
			want:        false,
		},
		{
			// Web / mobile chat carries no channel type and is answered by its
			// own branch in the prompt, not by this function. It must not be
			// reported here as a file-carrying channel.
			name:        "web chat is not answered here",
			channelType: "",
			want:        false,
		},
		{
			// A channel the server added that this daemon build has no constant
			// for. False is the safe direction: the agent describes the file in
			// words, and the worst case is a deliverable that could have been
			// sent was not. True would promise a delivery nothing performs.
			name:        "an unknown channel type is assumed text-only",
			channelType: "dingtalk",
			want:        false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ChannelCarriesFiles(tc.channelType); got != tc.want {
				t.Errorf("ChannelCarriesFiles(%q) = %v, want %v", tc.channelType, got, tc.want)
			}
		})
	}
}

// ChannelCarriesFiles and ChannelDisplayName are read together by every branch
// that mentions a channel by name, so a channel that carries files must also
// have a name to put in the sentence. A raw discriminator would render as
// "sends it into the wecom conversation".
func TestChannelCarryingFilesHasADisplayName(t *testing.T) {
	t.Parallel()
	for _, ct := range []string{ChannelTypeSlack, ChannelTypeFeishu, ChannelTypeWecom} {
		if !ChannelCarriesFiles(ct) {
			continue
		}
		if name := ChannelDisplayName(ct); name == ct {
			t.Errorf("channel %q carries files but has no display name — the brief would name it %q", ct, name)
		}
	}
}
