package metrics

import (
	"regexp"
	"strings"
)

type ModelPrice struct {
	Provider       string
	Model          string
	InputPerM      float64
	CacheReadPerM  float64
	CacheWritePerM float64
	OutputPerM     float64
}

var modelPrices = map[string]ModelPrice{
	"openai:gpt-5.5":                            {Provider: "openai", Model: "gpt-5.5", InputPerM: 5.00, CacheReadPerM: 0.50, CacheWritePerM: 5.00, OutputPerM: 30.00},
	"openai:gpt-5.4":                            {Provider: "openai", Model: "gpt-5.4", InputPerM: 2.50, CacheReadPerM: 0.25, CacheWritePerM: 2.50, OutputPerM: 15.00},
	"openai:gpt-5.4-mini":                       {Provider: "openai", Model: "gpt-5.4-mini", InputPerM: 0.75, CacheReadPerM: 0.075, CacheWritePerM: 0.75, OutputPerM: 4.50},
	"openai:gpt-5.3-codex":                      {Provider: "openai", Model: "gpt-5.3-codex", InputPerM: 1.75, CacheReadPerM: 0.175, CacheWritePerM: 1.75, OutputPerM: 14.00},
	"openai:gpt-5.2-codex":                      {Provider: "openai", Model: "gpt-5.2-codex", InputPerM: 1.75, CacheReadPerM: 0.175, CacheWritePerM: 1.75, OutputPerM: 14.00},
	"anthropic:claude-fable-5":                  {Provider: "anthropic", Model: "claude-fable-5", InputPerM: 10.00, CacheReadPerM: 1.00, CacheWritePerM: 12.50, OutputPerM: 50.00},
	"anthropic:claude-opus-4.8":                 {Provider: "anthropic", Model: "claude-opus-4.8", InputPerM: 5.00, CacheReadPerM: 0.50, CacheWritePerM: 6.25, OutputPerM: 25.00},
	"anthropic:claude-opus-4.7":                 {Provider: "anthropic", Model: "claude-opus-4.7", InputPerM: 5.00, CacheReadPerM: 0.50, CacheWritePerM: 6.25, OutputPerM: 25.00},
	"anthropic:claude-opus-4.6":                 {Provider: "anthropic", Model: "claude-opus-4.6", InputPerM: 5.00, CacheReadPerM: 0.50, CacheWritePerM: 6.25, OutputPerM: 25.00},
	"anthropic:claude-opus-4.5":                 {Provider: "anthropic", Model: "claude-opus-4.5", InputPerM: 5.00, CacheReadPerM: 0.50, CacheWritePerM: 6.25, OutputPerM: 25.00},
	"anthropic:claude-sonnet-4.6":               {Provider: "anthropic", Model: "claude-sonnet-4.6", InputPerM: 3.00, CacheReadPerM: 0.30, CacheWritePerM: 3.75, OutputPerM: 15.00},
	"anthropic:claude-sonnet-4.5":               {Provider: "anthropic", Model: "claude-sonnet-4.5", InputPerM: 3.00, CacheReadPerM: 0.30, CacheWritePerM: 3.75, OutputPerM: 15.00},
	"anthropic:claude-haiku-4.5":                {Provider: "anthropic", Model: "claude-haiku-4.5", InputPerM: 1.00, CacheReadPerM: 0.10, CacheWritePerM: 1.25, OutputPerM: 5.00},
	"deepseek:v4-pro":                           {Provider: "deepseek", Model: "v4-pro", InputPerM: 0.435, CacheReadPerM: 0.003625, CacheWritePerM: 0.435, OutputPerM: 0.87},
	"deepseek:v4-flash":                         {Provider: "deepseek", Model: "v4-flash", InputPerM: 0.14, CacheReadPerM: 0.0028, CacheWritePerM: 0.14, OutputPerM: 0.28},
	"kimi:k2.7-code":                            {Provider: "kimi", Model: "k2.7-code", InputPerM: 0.95, CacheReadPerM: 0.19, CacheWritePerM: 0.95, OutputPerM: 4.00},
	"kimi:k2.7-code-highspeed":                  {Provider: "kimi", Model: "k2.7-code-highspeed", InputPerM: 1.90, CacheReadPerM: 0.38, CacheWritePerM: 1.90, OutputPerM: 8.00},
	"kimi:k2.6":                                 {Provider: "kimi", Model: "k2.6", InputPerM: 0.95, CacheReadPerM: 0.16, CacheWritePerM: 0.95, OutputPerM: 4.00},
	"kimi:k2.5":                                 {Provider: "kimi", Model: "k2.5", InputPerM: 0.60, CacheReadPerM: 0.10, CacheWritePerM: 0.60, OutputPerM: 3.00},
	"minimax:m2.7":                              {Provider: "minimax", Model: "m2.7", InputPerM: 0.30, CacheReadPerM: 0.06, CacheWritePerM: 0.375, OutputPerM: 1.20},
	"minimax:m2.7-highspeed":                    {Provider: "minimax", Model: "m2.7-highspeed", InputPerM: 0.60, CacheReadPerM: 0.06, CacheWritePerM: 0.375, OutputPerM: 2.40},
	"google:gemini-3-flash":                     {Provider: "google", Model: "gemini-3-flash", InputPerM: 0.50, CacheReadPerM: 0.05, CacheWritePerM: 0.50, OutputPerM: 3.00},
	"google:gemini-3.1-pro":                     {Provider: "google", Model: "gemini-3.1-pro", InputPerM: 2.00, CacheReadPerM: 0.20, CacheWritePerM: 2.00, OutputPerM: 12.00},
	"google:gemini-3.1-pro-preview":             {Provider: "google", Model: "gemini-3.1-pro-preview", InputPerM: 2.00, CacheReadPerM: 0.20, CacheWritePerM: 2.00, OutputPerM: 12.00},
	"google:gemini-3.1-pro-preview-customtools": {Provider: "google", Model: "gemini-3.1-pro-preview-customtools", InputPerM: 2.00, CacheReadPerM: 0.20, CacheWritePerM: 2.00, OutputPerM: 12.00},
	"google:gemini-2.5-pro":                     {Provider: "google", Model: "gemini-2.5-pro", InputPerM: 1.25, CacheReadPerM: 0.125, CacheWritePerM: 1.25, OutputPerM: 10.00},
	"google:gemini-2.5-flash":                   {Provider: "google", Model: "gemini-2.5-flash", InputPerM: 0.30, CacheReadPerM: 0.03, CacheWritePerM: 0.30, OutputPerM: 2.50},
	"google:gemini-2.5-flash-lite":              {Provider: "google", Model: "gemini-2.5-flash-lite", InputPerM: 0.10, CacheReadPerM: 0.01, CacheWritePerM: 0.10, OutputPerM: 0.40},
}

var modelAliasRules = []struct {
	re       *regexp.Regexp
	priceKey string
}{
	{regexp.MustCompile(`(^|/|:)gpt-5[.-]5$|^gpt-5-5$`), "openai:gpt-5.5"},
	{regexp.MustCompile(`(^|/|:)gpt-5[.-]4($|-2026-03-05|-xhigh)`), "openai:gpt-5.4"},
	{regexp.MustCompile(`(^|/|:)gpt-5[.-]4-mini($|[^a-z0-9])`), "openai:gpt-5.4-mini"},
	{regexp.MustCompile(`(^|/|:)gpt-5[.-]3-codex$`), "openai:gpt-5.3-codex"},
	{regexp.MustCompile(`(^|/|:)gpt-5[.-]2-codex$`), "openai:gpt-5.2-codex"},
	{regexp.MustCompile(`claude-fable-5`), "anthropic:claude-fable-5"},
	{regexp.MustCompile(`claude-opus-4[-.]8`), "anthropic:claude-opus-4.8"},
	{regexp.MustCompile(`claude-opus-4[-.]7`), "anthropic:claude-opus-4.7"},
	{regexp.MustCompile(`claude-opus-4[-.]6`), "anthropic:claude-opus-4.6"},
	{regexp.MustCompile(`claude-opus-4[-.]5`), "anthropic:claude-opus-4.5"},
	{regexp.MustCompile(`claude-sonnet-4[-.]6|claude-4[-.]6-sonnet`), "anthropic:claude-sonnet-4.6"},
	{regexp.MustCompile(`claude-sonnet-4[-.]5|claude-4[-.]5-sonnet`), "anthropic:claude-sonnet-4.5"},
	{regexp.MustCompile(`claude-haiku-4[-.]5`), "anthropic:claude-haiku-4.5"},
	{regexp.MustCompile(`deepseek-v4-pro`), "deepseek:v4-pro"},
	{regexp.MustCompile(`deepseek-v4-flash|^deepseek-chat$|^deepseek-reasoner$`), "deepseek:v4-flash"},
	{regexp.MustCompile(`kimi-k2[.]7-code-highspeed`), "kimi:k2.7-code-highspeed"},
	{regexp.MustCompile(`kimi-k2[.]7-code`), "kimi:k2.7-code"},
	{regexp.MustCompile(`kimi-k2[.]6`), "kimi:k2.6"},
	{regexp.MustCompile(`kimi-k2[.]5`), "kimi:k2.5"},
	{regexp.MustCompile(`minimax-m2[.]7.*highspeed|highspeed.*minimax-m2[.]7`), "minimax:m2.7-highspeed"},
	{regexp.MustCompile(`minimax-m2[.]7`), "minimax:m2.7"},
	{regexp.MustCompile(`gemini-3[.]1-pro-preview-customtools`), "google:gemini-3.1-pro-preview-customtools"},
	{regexp.MustCompile(`gemini-3[.]1-pro-preview`), "google:gemini-3.1-pro-preview"},
	{regexp.MustCompile(`gemini-3[.]1-pro`), "google:gemini-3.1-pro"},
	{regexp.MustCompile(`gemini-3-flash-preview|gemini-3-flash`), "google:gemini-3-flash"},
	{regexp.MustCompile(`gemini-2[.]5-pro`), "google:gemini-2.5-pro"},
	{regexp.MustCompile(`gemini-2[.]5-flash-lite`), "google:gemini-2.5-flash-lite"},
	{regexp.MustCompile(`gemini-2[.]5-flash`), "google:gemini-2.5-flash"},
}

func PriceForModelAlias(model string) (ModelPrice, bool) {
	model = strings.ToLower(strings.TrimSpace(model))
	for _, rule := range modelAliasRules {
		if rule.re.MatchString(model) {
			price, ok := modelPrices[rule.priceKey]
			return price, ok
		}
	}
	return ModelPrice{}, false
}

func tokenCostUSD(tokens int64, pricePerM float64) float64 {
	if tokens <= 0 || pricePerM <= 0 {
		return 0
	}
	return float64(tokens) * pricePerM / 1_000_000
}
