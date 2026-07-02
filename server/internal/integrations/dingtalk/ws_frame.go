package dingtalk

// DingTalk Stream frames are JSON envelopes over the WebSocket. This file models
// the inbound DataFrame and the DataFrameResponse we write back, plus the small
// set of type/topic discriminators the chatbot connection uses. It replaces the
// former dependency on the vendor stream SDK's payload package.

const (
	frameTypeSystem   = "SYSTEM"
	frameTypeCallback = "CALLBACK"

	systemTopicPing       = "ping"
	systemTopicDisconnect = "disconnect"
	botMessageTopic       = "/v1.0/im/bot/messages/get"

	frameResponseCodeOK = 200
)

// dataFrame is an inbound Stream frame. The gateway carries the routing key in
// headers.topic and the correlation id in headers.messageId; data is the frame
// body as a JSON string (a bot callback for the bot-message topic).
type dataFrame struct {
	SpecVersion string            `json:"specVersion"`
	Type        string            `json:"type"`
	Headers     map[string]string `json:"headers"`
	Data        string            `json:"data"`
}

func (f *dataFrame) topic() string     { return f.Headers["topic"] }
func (f *dataFrame) messageID() string { return f.Headers["messageId"] }

// dataFrameResponse is the ack we write back for every frame. The gateway
// correlates it to the delivered frame by the echoed messageId header, so that
// header is load-bearing; data stays empty for a plain callback ack (the actual
// reply is delivered out-of-band over the Open API).
type dataFrameResponse struct {
	Code    int               `json:"code"`
	Headers map[string]string `json:"headers"`
	Message string            `json:"message"`
	Data    string            `json:"data"`
}

// newAckResponse builds a 200 ack echoing the frame's messageId.
func newAckResponse(messageID string) dataFrameResponse {
	return dataFrameResponse{
		Code:    frameResponseCodeOK,
		Message: "",
		Data:    "",
		Headers: map[string]string{
			"messageId":   messageID,
			"contentType": "application/json",
		},
	}
}

// newPongResponse answers a SYSTEM ping. It echoes the ping's messageId and its
// data body, mirroring the gateway's expected pong shape.
func newPongResponse(messageID, data string) dataFrameResponse {
	return dataFrameResponse{
		Code:    frameResponseCodeOK,
		Message: "ok",
		Data:    data,
		Headers: map[string]string{
			"messageId":   messageID,
			"contentType": "application/json",
		},
	}
}
