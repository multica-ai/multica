package recruitinbox

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/shared"

	"github.com/multica-ai/multica/server/pkg/llm"
)

const analyzerSystemPrompt = `You are the private recruitment intake analyzer. Return one JSON object only.
Extract only job-relevant facts: role, whether budget is present (never copy the amount), start date, owner, project lead, rule change, rule type, affected scope, missing fields, uncertainties, proposed next step, whether the instruction is consequential, and at most one clarification question.
Set consequential=true for candidate rejection, publishing a job, external contact, interview scheduling, an offer, salary or budget change, forwarding, or activating/changing a rule. Never claim an action was executed. Do not infer protected traits. Keep arrays concise. The word JSON is intentional.`

type OpenAIAnalyzer struct {
	client     *llm.Client
	model      string
	imageModel string
	audioModel string
}

func NewOpenAIAnalyzer(client *llm.Client, model, imageModel, audioModel string) (*OpenAIAnalyzer, error) {
	if client == nil || !client.Enabled() {
		return nil, errors.New("recruit inbox: OpenAI client is not configured")
	}
	if strings.TrimSpace(model) == "" {
		model = client.DefaultModel()
	}
	if strings.TrimSpace(imageModel) == "" {
		imageModel = model
	}
	if strings.TrimSpace(audioModel) == "" {
		audioModel = "gpt-4o-mini-transcribe"
	}
	return &OpenAIAnalyzer{client: client, model: model, imageModel: imageModel, audioModel: audioModel}, nil
}

func (a *OpenAIAnalyzer) AnalyzeText(ctx context.Context, text string) (Extraction, error) {
	raw, err := a.client.GenerateJSON(ctx, a.model, analyzerSystemPrompt, text, 0, 1600)
	if err != nil {
		return Extraction{}, err
	}
	return decodeExtraction(raw)
}

func (a *OpenAIAnalyzer) AnalyzeImage(ctx context.Context, image []byte, contentType string) (Extraction, error) {
	if contentType == "" {
		contentType = "image/jpeg"
	}
	dataURL := "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(image)
	params := openai.ChatCompletionNewParams{
		Model: shared.ChatModel(a.imageModel),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(analyzerSystemPrompt),
			openai.UserMessage([]openai.ChatCompletionContentPartUnionParam{
				openai.TextContentPart("OCR the image and analyze its recruitment content. Return JSON only."),
				openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{URL: dataURL, Detail: "high"}),
			}),
		},
		ResponseFormat:      openai.ChatCompletionNewParamsResponseFormatUnion{OfJSONObject: &shared.ResponseFormatJSONObjectParam{}},
		MaxCompletionTokens: openai.Int(1600),
		Store:               openai.Bool(false),
	}
	if isGPT56Model(a.imageModel) {
		params.ReasoningEffort = shared.ReasoningEffortNone
	}
	completion, err := a.client.Chat(ctx, params)
	if err != nil {
		return Extraction{}, err
	}
	if len(completion.Choices) == 0 {
		return Extraction{}, errors.New("recruit inbox: image analyzer returned no choices")
	}
	return decodeExtraction(completion.Choices[0].Message.Content)
}

func (a *OpenAIAnalyzer) AnalyzeFile(ctx context.Context, data []byte, filename, contentType string) (Extraction, error) {
	if strings.HasPrefix(strings.ToLower(contentType), "text/") || hasTextExtension(filename) {
		return a.AnalyzeText(ctx, string(data))
	}
	params := openai.ChatCompletionNewParams{
		Model: shared.ChatModel(a.imageModel),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(analyzerSystemPrompt),
			openai.UserMessage([]openai.ChatCompletionContentPartUnionParam{
				openai.TextContentPart("Analyze this supported recruitment document. Return JSON only."),
				openai.FileContentPart(openai.ChatCompletionContentPartFileFileParam{
					FileData: param.NewOpt(base64.StdEncoding.EncodeToString(data)),
					Filename: param.NewOpt(filename),
				}),
			}),
		},
		ResponseFormat:      openai.ChatCompletionNewParamsResponseFormatUnion{OfJSONObject: &shared.ResponseFormatJSONObjectParam{}},
		MaxCompletionTokens: openai.Int(1600),
		Store:               openai.Bool(false),
	}
	if isGPT56Model(a.imageModel) {
		params.ReasoningEffort = shared.ReasoningEffortNone
	}
	completion, err := a.client.Chat(ctx, params)
	if err != nil {
		return Extraction{}, err
	}
	if len(completion.Choices) == 0 {
		return Extraction{}, errors.New("recruit inbox: file analyzer returned no choices")
	}
	return decodeExtraction(completion.Choices[0].Message.Content)
}

func hasTextExtension(filename string) bool {
	filename = strings.ToLower(strings.TrimSpace(filename))
	return strings.HasSuffix(filename, ".txt") || strings.HasSuffix(filename, ".md") || strings.HasSuffix(filename, ".csv")
}

func isGPT56Model(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return model == "gpt-5.6" || strings.HasPrefix(model, "gpt-5.6-")
}

func (a *OpenAIAnalyzer) Transcribe(ctx context.Context, audio io.Reader, filename string) (string, error) {
	data, err := io.ReadAll(audio)
	if err != nil {
		return "", err
	}
	response, err := a.client.SDK().Audio.Transcriptions.New(ctx, openai.AudioTranscriptionNewParams{
		File:           openai.File(bytes.NewReader(data), filename, "application/octet-stream"),
		Model:          openai.AudioModel(a.audioModel),
		Language:       param.NewOpt("zh"),
		ResponseFormat: "json",
	})
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(response.Text) == "" {
		return "", errors.New("recruit inbox: transcription was empty")
	}
	return response.Text, nil
}

func decodeExtraction(raw string) (Extraction, error) {
	var out Extraction
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return Extraction{}, fmt.Errorf("recruit inbox: decode analysis: %w", err)
	}
	if strings.ContainsAny(out.Clarification, "\r\n") {
		out.Clarification = strings.TrimSpace(strings.Split(strings.ReplaceAll(out.Clarification, "\r\n", "\n"), "\n")[0])
	}
	if len(out.MissingFields) > 12 {
		out.MissingFields = out.MissingFields[:12]
	}
	if len(out.Uncertainties) > 12 {
		out.Uncertainties = out.Uncertainties[:12]
	}
	return out, nil
}
