package dingtalk

import (
	"encoding/json"
	"testing"
)

func TestDataFrame_Accessors(t *testing.T) {
	raw := `{"specVersion":"1.0","type":"CALLBACK","headers":{"topic":"/v1.0/im/bot/messages/get","messageId":"mid-1","contentType":"application/json"},"data":"{\"msgId\":\"m\"}"}`
	var f dataFrame
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if f.Type != frameTypeCallback {
		t.Errorf("type = %q, want %q", f.Type, frameTypeCallback)
	}
	if f.topic() != botMessageTopic {
		t.Errorf("topic = %q, want %q", f.topic(), botMessageTopic)
	}
	if f.messageID() != "mid-1" {
		t.Errorf("messageID = %q, want mid-1", f.messageID())
	}
	if f.Data != `{"msgId":"m"}` {
		t.Errorf("data = %q", f.Data)
	}
}

func TestNewAckResponse_EchoesMessageID(t *testing.T) {
	ack := newAckResponse("mid-9")
	if ack.Code != frameResponseCodeOK {
		t.Errorf("code = %d, want %d", ack.Code, frameResponseCodeOK)
	}
	if ack.Headers["messageId"] != "mid-9" {
		t.Errorf("messageId = %q, want mid-9", ack.Headers["messageId"])
	}
	if ack.Headers["contentType"] != "application/json" {
		t.Errorf("contentType = %q", ack.Headers["contentType"])
	}
	if ack.Data != "" {
		t.Errorf("ack data must be empty, got %q", ack.Data)
	}
}

func TestNewPongResponse_EchoesData(t *testing.T) {
	pong := newPongResponse("mid-2", `{"k":"v"}`)
	if pong.Message != "ok" {
		t.Errorf("message = %q, want ok", pong.Message)
	}
	if pong.Data != `{"k":"v"}` {
		t.Errorf("pong must echo data, got %q", pong.Data)
	}
	if pong.Headers["messageId"] != "mid-2" {
		t.Errorf("messageId = %q, want mid-2", pong.Headers["messageId"])
	}
}
