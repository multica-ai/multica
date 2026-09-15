package dingtalk

import (
	"context"
	"errors"
	"fmt"
)

// DingTalk's built-in emoji reactions are addressed by their platform-defined
// names, not by installation-specific IDs. The Chinese acknowledgement below
// is therefore an OpenAPI enum value (emotion_167), not user-facing copy owned
// by Multica. Done is emotion_193.
const (
	emotionAcknowledged = "收到"
	emotionDone         = "Done"

	pathReplyEmotion  = "/v1.0/robot/emotion/reply"
	pathRecallEmotion = "/v1.0/robot/emotion/recall"
)

// setEmojiReaction adds or recalls one default emoji reaction on a group or
// direct message. A 401 invalidates the installation token and retries once,
// matching message sends.
func (s *sender) setEmojiReaction(ctx context.Context, target sendTarget, name string, recall bool) error {
	if target.ConversationID == "" || target.SourceMessageID == "" {
		return errors.New("dingtalk: emoji reaction requires conversation id and message id")
	}
	if s.robotCode == "" {
		return errors.New("dingtalk: emoji reaction requires robot code")
	}
	if name != emotionAcknowledged && name != emotionDone {
		return fmt.Errorf("dingtalk: unsupported emoji reaction %q", name)
	}
	path := pathReplyEmotion
	if recall {
		path = pathRecallEmotion
	}
	body := map[string]any{
		"robotCode":          s.robotCode,
		"openConversationId": target.ConversationID,
		"openMsgId":          target.SourceMessageID,
		"emotionType":        1,
		"emotionName":        name,
	}
	for attempt := 0; attempt < 2; attempt++ {
		token, err := s.client.accessToken(ctx, s.appKey, s.appSecret)
		if err != nil {
			return fmt.Errorf("access token: %w", err)
		}
		var resp struct {
			Success bool `json:"success"`
		}
		err = s.client.postJSON(ctx, path, token, body, &resp)
		if err == nil {
			if !resp.Success {
				return fmt.Errorf("dingtalk: %s returned success=false", path)
			}
			return nil
		}
		if errors.Is(err, errUnauthorized) && attempt == 0 {
			s.client.invalidate(s.appKey)
			continue
		}
		return err
	}
	return errUnauthorized
}
