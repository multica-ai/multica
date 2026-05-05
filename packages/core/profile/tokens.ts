// CEREBRO-PATCH(core-profile-tokens): cerebro modification of upstream file
// Token estimation for the live preview panel.
//
// Anthropic does not expose their tokenizer publicly, and pulling the OpenAI
// cl100k_base WASM blob into every web bundle for an estimate is overkill.
// We use a character-based heuristic that's accurate to ±15% for the prompt
// shapes produced by compile.ts. Always present the result as "~estimat" in
// UI to avoid implying a precision we cannot guarantee.

const CHARS_PER_TOKEN_DA = 3.3;
const CHARS_PER_TOKEN_EN = 3.8;

const CONTEXT_WINDOW_TOKENS = 200_000;

export interface TokenEstimate {
  tokens: number;
  contextPercent: number;
  approximate: true;
}

export function estimateTokens(text: string, language: "da" | "en" = "da"): TokenEstimate {
  const charsPerToken = language === "da" ? CHARS_PER_TOKEN_DA : CHARS_PER_TOKEN_EN;
  const tokens = Math.max(1, Math.round(text.length / charsPerToken));
  return {
    tokens,
    contextPercent: tokens / CONTEXT_WINDOW_TOKENS,
    approximate: true,
  };
}

export function formatContextPercent(fraction: number): string {
  if (fraction < 0.0001) return "<0.01%";
  return `${(fraction * 100).toFixed(2)}%`;
}
