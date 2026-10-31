package telegram

import "testing"

func TestPrivateMessagesPreserveLiteralBotMentions(t *testing.T) {
	for _, text := range []string{
		"当然是 @IvangelionAI_bot 可以继续",
		"当然是\n@IvangelionAI_bot\n可以继续",
		"😀 @IvangelionAI_bot and @another_bot",
		"@IvangelionAI_bot",
	} {
		for _, caption := range []bool{false, true} {
			m := &Message{MessageID: 7, From: &User{ID: 111}, Chat: Chat{ID: 111, Type: "private"}}
			want := text
			if caption {
				m.Caption = text
				m.Photo = []PhotoSize{{FileID: "fake-photo"}}
				want = "[Image]\n" + text
			} else {
				m.Text = text
			}
			msg, ok := inboundFromUpdate(Update{Message: m}, 999, "IvangelionAI_bot")
			if !ok || msg.Text != want || msg.CommandText != text {
				t.Errorf("caption=%v: got text=%q command=%q; want text=%q command=%q", caption, msg.Text, msg.CommandText, want, text)
			}
		}
	}
}

func TestPrivateCommandsStripOnlyRoutingSuffix(t *testing.T) {
	for _, tc := range []struct {
		input, command, body string
		fresh                bool
	}{
		{"/issue@my_bot fix @my_bot", "/issue fix @my_bot", "/issue fix @my_bot", false},
		{"/clear@MY_BOT continue @my_bot", "/clear continue @my_bot", "continue @my_bot", true},
		{"/issue@another_bot mention @my_bot", "/issue@another_bot mention @my_bot", "/issue@another_bot mention @my_bot", false},
	} {
		msg, ok := inboundFromUpdate(Update{Message: &Message{
			MessageID: 8, From: &User{ID: 111}, Chat: Chat{ID: 111, Type: "private"}, Text: tc.input,
		}}, 999, "my_bot")
		if !ok || msg.CommandText != tc.command || msg.Text != tc.body || msg.ForceFresh != tc.fresh {
			t.Errorf("%q: command=%q text=%q fresh=%v", tc.input, msg.CommandText, msg.Text, msg.ForceFresh)
		}
	}
}
