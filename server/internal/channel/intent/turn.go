package intent

import "strings"

type ChannelTurnMode string

const (
	ChannelTurnModeQuery    ChannelTurnMode = "query"
	ChannelTurnModeMutation ChannelTurnMode = "mutation"
	ChannelTurnModeReply    ChannelTurnMode = "reply"
	ChannelTurnModeClarify  ChannelTurnMode = "clarify"
	ChannelTurnModeIgnore   ChannelTurnMode = "ignore"
)

type ChannelAction struct {
	Intent            IntentKind        `json:"intent"`
	Target            string            `json:"target,omitempty"`
	Params            map[string]string `json:"params,omitempty"`
	NeedsConfirmation bool              `json:"needs_confirmation,omitempty"`
}

type ChannelClarification struct {
	Question string   `json:"question,omitempty"`
	Missing  []string `json:"missing,omitempty"`
}

type ChannelTurnPlan struct {
	Mode           ChannelTurnMode      `json:"mode"`
	Intent         IntentKind           `json:"intent"`
	Target         string               `json:"target,omitempty"`
	Params         map[string]string    `json:"params,omitempty"`
	NeedsConfirm   bool                 `json:"needs_confirmation,omitempty"`
	UserReplyDraft string               `json:"user_reply_draft,omitempty"`
	Clarification  ChannelClarification `json:"clarification,omitempty"`
	Confidence     float64              `json:"confidence,omitempty"`
}

type ChannelComposeRequest struct {
	Plan          ChannelTurnPlan `json:"plan"`
	ExecutionJSON string          `json:"execution_json,omitempty"`
}

func IntentFromTurnPlan(plan ChannelTurnPlan, sourceText string) IntentResult {
	kind := plan.Intent
	if kind == "" && plan.Mode == ChannelTurnModeClarify {
		kind = IntentASKClarify
	}
	if kind == "" && plan.Mode == ChannelTurnModeIgnore {
		kind = IntentUnknown
	}
	if kind == IntentDelete {
		kind = IntentUnsupported
	}
	params := plan.Params
	if params == nil {
		params = map[string]string{}
	}
	if strings.TrimSpace(plan.Target) != "" && strings.TrimSpace(params["issue_key"]) == "" {
		params["issue_key"] = keyParam(plan.Target)
	}
	if strings.TrimSpace(plan.UserReplyDraft) != "" {
		params["_user_reply_draft"] = strings.TrimSpace(plan.UserReplyDraft)
	}
	confidence := plan.Confidence
	if confidence == 0 {
		confidence = 0.9
	}
	result := Intent{Kind: kind, Confidence: confidence, Params: params, Source: SourceChat}
	result = refineChatIntentWithSourceText(result, sourceText)
	if !intentHasRequiredParams(result) {
		result = fallbackIntent(IntentASKClarify)
		if strings.TrimSpace(plan.UserReplyDraft) != "" {
			result.Params["_user_reply_draft"] = strings.TrimSpace(plan.UserReplyDraft)
		}
	}
	result.Source = SourceChat
	return IntentResult{Matched: true, Intent: result}
}
