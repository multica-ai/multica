package wecom

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
)

type wsFrame struct {
	Cmd     string          `json:"cmd,omitempty"`
	Headers wsHeaders       `json:"headers"`
	Body    json.RawMessage `json:"body,omitempty"`
	ErrCode int             `json:"errcode,omitempty"`
	ErrMsg  string          `json:"errmsg,omitempty"`
}

type wsHeaders struct {
	ReqID string `json:"req_id"`
}

type msgCallbackBody struct {
	MsgID    string `json:"msgid"`
	AibotID  string `json:"aibotid"`
	ChatID   string `json:"chatid"`
	ChatType string `json:"chattype"`
	From     struct {
		Userid string `json:"userid"`
	} `json:"from"`
	MsgType string `json:"msgtype"`
	Text    struct {
		Content string `json:"content"`
	} `json:"text"`
	Mixed struct {
		MsgItem []struct {
			MsgType string `json:"msgtype"`
			Text    struct {
				Content string `json:"content"`
			} `json:"text"`
		} `json:"msg_item"`
	} `json:"mixed"`
}

type streamReplyBody struct {
	MsgType string `json:"msgtype"`
	Stream  struct {
		ID      string `json:"id"`
		Finish  bool   `json:"finish"`
		Content string `json:"content"`
	} `json:"stream"`
}

func newReqID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func buildSubscribeFrame(botID, secret string) ([]byte, error) {
	frame := wsFrame{
		Cmd: "aibot_subscribe",
		Headers: wsHeaders{ReqID: newReqID()},
	}
	body, _ := json.Marshal(map[string]string{
		"bot_id": botID,
		"secret": secret,
	})
	frame.Body = body
	return json.Marshal(frame)
}

func buildStreamReply(reqID, streamID, content string, finish bool) ([]byte, error) {
	var body streamReplyBody
	body.MsgType = "stream"
	body.Stream.ID = streamID
	body.Stream.Finish = finish
	body.Stream.Content = content
	raw, _ := json.Marshal(body)
	frame := wsFrame{
		Cmd:     "aibot_respond_msg",
		Headers: wsHeaders{ReqID: reqID},
		Body:    raw,
	}
	return json.Marshal(frame)
}

func extractMessageBody(b msgCallbackBody) string {
	switch b.MsgType {
	case "text":
		return b.Text.Content
	case "mixed":
		var parts []string
		for _, item := range b.Mixed.MsgItem {
			if item.MsgType == "text" && item.Text.Content != "" {
				parts = append(parts, item.Text.Content)
			}
		}
		if len(parts) == 0 {
			return ""
		}
		out := parts[0]
		for i := 1; i < len(parts); i++ {
			out += "\n" + parts[i]
		}
		return out
	default:
		return ""
	}
}
