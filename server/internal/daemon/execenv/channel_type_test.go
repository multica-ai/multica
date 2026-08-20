package execenv

import "testing"

// ChannelCarriesFiles is the delivery half of the MUL-4899 channel policy. It
// decides whether an agent is told to run `multica attachment upload` or to
// describe its file in words, so a wrong answer is not cosmetic: a false
// positive has the agent write "see the attached chart" into a conversation
// that never receives one.
//
// The property under test is that the answer comes from the SERVER and from
// nothing else. Every row pairs a channel type with a verdict, and the two are
// deliberately crossed — a channel whose adapter performs the hop but whose
// deployment has no storage answers false, and a channel this package has no
// opinion about answers true when the server says so. A build that decides
// from the channel type can satisfy at most half of them.
func TestChannelCarriesFiles(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		channelType   string
		serverVerdict bool
		want          bool
	}{
		{
			// The whole hop is present: the adapter goes back for the bound
			// attachment and the deployment has the object storage it goes
			// back to.
			name:          "wecom, on a deployment that can deliver",
			channelType:   ChannelTypeWecom,
			serverVerdict: true,
			want:          true,
		},
		{
			// The same adapter on a deployment with no object storage. There
			// is nothing to read the attachment out of, so the file cannot
			// travel, and this is the row that an inference from the channel
			// type gets wrong — it sees "wecom" and promises a delivery.
			name:          "wecom, on a deployment with no object storage",
			channelType:   ChannelTypeWecom,
			serverVerdict: false,
			want:          false,
		},
		{
			// A daemon new enough to know about WeCom file delivery talking to
			// a server too old to perform it. The field is absent from the
			// claim, which decodes as false, and false is the brief that tells
			// the agent to describe its file in words.
			name:          "wecom, against a server that predates the capability",
			channelType:   ChannelTypeWecom,
			serverVerdict: false,
			want:          false,
		},
		{
			name:          "slack does not carry files",
			channelType:   ChannelTypeSlack,
			serverVerdict: false,
			want:          false,
		},
		{
			name:          "feishu does not carry files",
			channelType:   ChannelTypeFeishu,
			serverVerdict: false,
			want:          false,
		},
		{
			// If a Slack deployment ever grows the hop, the server says so and
			// the agent is told, without this package being rebuilt. An
			// allowlist here would have to be edited to allow it, which is the
			// same coupling in the other direction.
			name:          "slack, on a server that says it can deliver",
			channelType:   ChannelTypeSlack,
			serverVerdict: true,
			want:          true,
		},
		{
			// Web / mobile chat carries no channel type and is answered by its
			// own branch in the prompt, which points at the attachment card the
			// browser renders. It must not be reported here whatever arrives
			// alongside it.
			name:          "web chat is not answered here",
			channelType:   "",
			serverVerdict: true,
			want:          false,
		},
		{
			// A channel type this daemon build has no constant for. The server
			// knows what it wired; this package does not need to.
			name:          "an unknown channel type still takes the server's word",
			channelType:   "dingtalk",
			serverVerdict: true,
			want:          true,
		},
		{
			name:          "an unknown channel type with no verdict is text-only",
			channelType:   "dingtalk",
			serverVerdict: false,
			want:          false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ChannelCarriesFiles(tc.channelType, tc.serverVerdict); got != tc.want {
				t.Errorf("ChannelCarriesFiles(%q, %v) = %v, want %v",
					tc.channelType, tc.serverVerdict, got, tc.want)
			}
		})
	}
}

// The copy for a file-carrying channel names it — "sends it into the %s
// conversation" — so every channel type this package knows must render as a
// name rather than as its wire discriminator. A missing mapping reads as "sends
// it into the wecom conversation".
func TestEveryKnownChannelHasADisplayName(t *testing.T) {
	t.Parallel()
	for _, ct := range []string{ChannelTypeSlack, ChannelTypeFeishu, ChannelTypeWecom} {
		if name := ChannelDisplayName(ct); name == ct {
			t.Errorf("channel %q has no display name — the brief would name it %q", ct, name)
		}
	}
}
