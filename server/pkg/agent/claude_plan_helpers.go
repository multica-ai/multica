package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Plan approval helpers shared between Claude transport implementations.

func shouldAbortPlanApproval(chosen string, err error) bool {
	chosen = strings.ToLower(strings.TrimSpace(chosen))
	if err != nil {
		return true
	}
	switch chosen {
	case "deny", "reject", "decline", "cancel", "stop", "timeout", "timed_out", "cancelled":
		return true
	default:
		return false
	}
}

func planApprovalAbortMessage(chosen string, err error) string {
	if err != nil {
		return fmt.Sprintf("plan approval did not receive a response: %v", err)
	}
	switch strings.ToLower(strings.TrimSpace(chosen)) {
	case "timeout", "timed_out":
		return "plan approval timed out before a response"
	case "cancelled":
		return "plan approval was cancelled"
	default:
		return "plan rejected by user"
	}
}

func emitPlanApprovalStage(trace TraceCallback, chosen string, approved bool, responseMessage string, language string) {
	language = normalizedVisibleLanguage(language)
	switch strings.ToLower(strings.TrimSpace(chosen)) {
	case "allow", "accept_similar":
		emitDisplayEvent(trace, "plan_stage", "Plan accepted", localizedPlanStageContent(language, "accepted", responseMessage), map[string]any{
			"stage": "executing",
		})
	case "revise", "keep_planning":
		emitDisplayEvent(trace, "plan_stage", "Plan revision requested", localizedPlanStageContent(language, "revise", responseMessage), map[string]any{
			"stage": "planning",
		})
	default:
		if approved {
			return
		}
		stage := "rejected"
		title := "Plan rejected"
		content := "The plan was rejected and Claude should stop this plan run."
		switch strings.ToLower(strings.TrimSpace(chosen)) {
		case "timeout", "timed_out":
			stage = "expired"
			title = "Plan approval expired"
			content = localizedPlanStageContent(language, "expired", responseMessage)
		case "cancelled":
			stage = "cancelled"
			title = "Plan approval cancelled"
			content = localizedPlanStageContent(language, "cancelled", responseMessage)
		default:
			content = localizedPlanStageContent(language, "rejected", responseMessage)
		}
		emitDisplayEvent(trace, "plan_stage", title, content, map[string]any{
			"stage": stage,
		})
	}
}

func normalizedVisibleLanguage(language string) string {
	language = strings.ToLower(strings.TrimSpace(language))
	switch {
	case strings.HasPrefix(language, "zh"):
		return "zh"
	default:
		return "en"
	}
}

func localizedPlanStageContent(language, kind, responseMessage string) string {
	language = normalizedVisibleLanguage(language)
	responseMessage = strings.TrimSpace(responseMessage)
	if language == "zh" {
		switch kind {
		case "accepted":
			return "Claude 已退出计划模式，并将在这次运行中继续执行。"
		case "revise":
			content := "Claude 将继续停留在计划模式中修改方案。"
			if responseMessage != "" {
				content += "\n\n修改要求：\n" + responseMessage
			}
			return content
		case "expired":
			return "该计划确认长时间未处理，本次运行已停止，且不会向 Claude 回写拒绝。"
		case "cancelled":
			return "该计划确认已被取消，本次运行已停止，且不会向 Claude 回写拒绝。"
		default:
			return "该计划已被拒绝，Claude 将停止这次计划运行。"
		}
	}
	switch kind {
	case "accepted":
		return "Claude exited plan mode and is continuing execution in this run."
	case "revise":
		content := "Claude is staying in plan mode to revise the plan."
		if responseMessage != "" {
			content += "\n\nRevision request:\n" + responseMessage
		}
		return content
	case "expired":
		return "The plan approval was not answered in time, so this run was stopped without sending a rejection back to Claude."
	case "cancelled":
		return "The plan approval was cancelled, so this run was stopped without sending a rejection back to Claude."
	default:
		return "The plan was rejected and Claude should stop this plan run."
	}
}

func mergeApprovedPlanIntoOutput(language, approvedPlan, executionOutput string) string {
	approvedPlan = strings.TrimSpace(approvedPlan)
	executionOutput = strings.TrimSpace(executionOutput)
	if approvedPlan == "" {
		return executionOutput
	}
	if executionOutput == "" {
		return approvedPlan
	}
	if strings.Contains(executionOutput, approvedPlan) {
		return executionOutput
	}
	if normalizedVisibleLanguage(language) == "zh" {
		return "已批准方案：\n\n" + approvedPlan + "\n\n执行结果：\n\n" + executionOutput
	}
	return "Approved plan:\n\n" + approvedPlan + "\n\nExecution result:\n\n" + executionOutput
}

// AskUserQuestion parsing types and builder.

type claudeSDKQuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

type claudeSDKQuestion struct {
	Header   string                    `json:"header"`
	Question string                    `json:"question"`
	Options  []claudeSDKQuestionOption `json:"options"`
}

type claudeSDKAskUserQuestionInput struct {
	Questions []claudeSDKQuestion `json:"questions"`
}

func buildClaudeSDKUserInputRequest(raw json.RawMessage) ApprovalRequest {
	req := ApprovalRequest{
		Type:          protocol.InteractionUserInputRequest,
		Title:         "Question from Claude",
		DefaultOption: "",
	}

	var payload claudeSDKAskUserQuestionInput
	if len(raw) == 0 || json.Unmarshal(raw, &payload) != nil || len(payload.Questions) == 0 {
		req.Detail = strings.TrimSpace(string(raw))
		return req
	}

	question := payload.Questions[0]
	if header := strings.TrimSpace(question.Header); header != "" {
		req.Title = header
	}

	var detail strings.Builder
	if text := strings.TrimSpace(question.Question); text != "" {
		detail.WriteString(text)
	}
	if len(question.Options) > 0 {
		if detail.Len() > 0 {
			detail.WriteString("\n\n")
		}
		detail.WriteString("Options:")
		for i, opt := range question.Options {
			label := strings.TrimSpace(opt.Label)
			if label == "" {
				label = fmt.Sprintf("Option %d", i+1)
			}
			req.Options = append(req.Options, protocol.InteractionOption{ID: label, Label: label})
			if req.DefaultOption == "" {
				req.DefaultOption = label
			}
			detail.WriteString("\n- ")
			detail.WriteString(label)
			if desc := strings.TrimSpace(opt.Description); desc != "" {
				detail.WriteString(": ")
				detail.WriteString(desc)
			}
		}
	}
	if len(payload.Questions) > 1 {
		if detail.Len() > 0 {
			detail.WriteString("\n\n")
		}
		detail.WriteString(fmt.Sprintf("Additional questions omitted: %d", len(payload.Questions)-1))
	}
	req.Detail = detail.String()
	return req
}

func extractExitPlanText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text)
	}
	var record map[string]any
	if err := json.Unmarshal(raw, &record); err != nil {
		return ""
	}
	for _, key := range []string{"plan", "content", "summary", "message"} {
		if value, ok := record[key]; ok {
			if s := strings.TrimSpace(fmt.Sprint(value)); s != "" {
				return s
			}
		}
	}
	return ""
}
