package dingtalk

import "testing"

func TestParseFreshSessionCommand(t *testing.T) {
	cases := []struct {
		body     string
		wantBody string
		wantOK   bool
	}{
		{"/new", "", true},
		{"/new 帮我看看这个报错", "帮我看看这个报错", true},
		{"  /new  hello ", "hello", true},
		{"\n\n/new next line\nmore", "next line\nmore", true},
		{"/new\n后续内容", "后续内容", true},
		{"/newx", "", false},
		{"/New", "", false},
		{"say /new inline", "", false},
		{"hello\n/new", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := parseFreshSessionCommand(c.body)
		if ok != c.wantOK || got != c.wantBody {
			t.Errorf("parseFreshSessionCommand(%q) = (%q, %v), want (%q, %v)", c.body, got, ok, c.wantBody, c.wantOK)
		}
	}
}

func TestInboundNewCommandForcesFresh(t *testing.T) {
	msg, ok := inboundFromBotCallback(botCallbackData{
		ConversationID:   "cid",
		MsgID:            "m1",
		SenderStaffID:    "staff_1",
		ConversationType: "1",
		Msgtype:          "text",
		Text: struct {
			Content string `json:"content"`
		}{Content: "/new 重新开始"},
	}, "client_a")
	if !ok {
		t.Fatal("inbound mapping failed")
	}
	if !msg.ForceFresh {
		t.Fatal("expected ForceFresh=true for /new")
	}
	if msg.Text != "重新开始" {
		t.Fatalf("expected stripped body, got %q", msg.Text)
	}
}

func TestInboundPlainTextDoesNotForceFresh(t *testing.T) {
	msg, ok := inboundFromBotCallback(botCallbackData{
		ConversationID:   "cid",
		MsgID:            "m1",
		SenderStaffID:    "staff_1",
		ConversationType: "1",
		Msgtype:          "text",
		Text: struct {
			Content string `json:"content"`
		}{Content: "你好"},
	}, "client_a")
	if !ok {
		t.Fatal("inbound mapping failed")
	}
	if msg.ForceFresh {
		t.Fatal("plain text must not force fresh")
	}
}
