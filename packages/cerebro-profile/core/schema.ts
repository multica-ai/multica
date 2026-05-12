// CEREBRO-PATCH(core-profile-schema): cerebro modification of upstream file
// User communication profile (JEH-304 / JEH-1031). Mirrors the user_profile
// table. Per JEH-1031 the single tech slider was replaced by four 1-5 scope
// ratings (git / code / computer / process) and a custom-prompt escape hatch
// with append/replace modes.

export const PERSONAS = ["utalmodig", "ekspert", "grundig", "larling"] as const;
export type Persona = (typeof PERSONAS)[number];

export const LANGUAGES = ["da", "en"] as const;
export type Language = (typeof LANGUAGES)[number];

export const PROMPT_MODES = ["append", "replace"] as const;
export type PromptMode = (typeof PROMPT_MODES)[number];

// Length / autonomy keep the legacy 0-100 sliders; only the tech axis was
// replaced by per-dimension 1-5 questions.
export const SLIDER_MIN = 0;
export const SLIDER_MAX = 100;

export const SCOPE_MIN = 1;
export const SCOPE_MAX = 5;

export const ANTI_PATTERNS_MAX_COUNT = 20;
export const ANTI_PATTERN_MAX_LENGTH = 100;

export const CUSTOM_PROMPT_MAX_LENGTH = 4000;

// Hard cap on compiled prompt size. Enforced server-side; UI shows a warning
// before the user hits it. Aligned with the issue's <200 token target.
export const COMPILED_PROMPT_TOKEN_CAP = 200;

export const SCOPE_FIELDS = ["gitPref", "codePref", "computerPref", "processPref"] as const;
export type ScopeField = (typeof SCOPE_FIELDS)[number];

export interface Profile {
  persona: Persona;
  language: Language;
  lengthPref: number;
  autonomyPref: number;
  gitPref: number;
  codePref: number;
  computerPref: number;
  processPref: number;
  antiPatterns: string[];
  customPrompt: string;
  promptMode: PromptMode;
}

export interface ProfileRow {
  user_id: string;
  persona: Persona;
  language: Language;
  length_pref: number;
  autonomy_pref: number;
  git_pref: number;
  code_pref: number;
  computer_pref: number;
  process_pref: number;
  anti_patterns: string[];
  custom_prompt: string;
  prompt_mode: PromptMode;
  updated_at: string;
}

export type ProfileValidationError =
  | { kind: "invalid_persona"; got: string }
  | { kind: "invalid_language"; got: string }
  | { kind: "invalid_prompt_mode"; got: string }
  | { kind: "slider_out_of_range"; field: "lengthPref" | "autonomyPref"; got: number }
  | { kind: "scope_out_of_range"; field: ScopeField; got: number }
  | { kind: "too_many_anti_patterns"; count: number; max: number }
  | { kind: "anti_pattern_too_long"; index: number; length: number; max: number }
  | { kind: "custom_prompt_too_long"; length: number; max: number };

export function validateProfile(p: Profile): ProfileValidationError[] {
  const errors: ProfileValidationError[] = [];

  if (!PERSONAS.includes(p.persona)) {
    errors.push({ kind: "invalid_persona", got: p.persona });
  }
  if (!LANGUAGES.includes(p.language)) {
    errors.push({ kind: "invalid_language", got: p.language });
  }
  if (!PROMPT_MODES.includes(p.promptMode)) {
    errors.push({ kind: "invalid_prompt_mode", got: p.promptMode });
  }

  for (const field of ["lengthPref", "autonomyPref"] as const) {
    const value = p[field];
    if (!Number.isInteger(value) || value < SLIDER_MIN || value > SLIDER_MAX) {
      errors.push({ kind: "slider_out_of_range", field, got: value });
    }
  }

  for (const field of SCOPE_FIELDS) {
    const value = p[field];
    if (!Number.isInteger(value) || value < SCOPE_MIN || value > SCOPE_MAX) {
      errors.push({ kind: "scope_out_of_range", field, got: value });
    }
  }

  if (p.antiPatterns.length > ANTI_PATTERNS_MAX_COUNT) {
    errors.push({
      kind: "too_many_anti_patterns",
      count: p.antiPatterns.length,
      max: ANTI_PATTERNS_MAX_COUNT,
    });
  }

  p.antiPatterns.forEach((pattern, index) => {
    if (pattern.length > ANTI_PATTERN_MAX_LENGTH) {
      errors.push({
        kind: "anti_pattern_too_long",
        index,
        length: pattern.length,
        max: ANTI_PATTERN_MAX_LENGTH,
      });
    }
  });

  if (p.customPrompt.length > CUSTOM_PROMPT_MAX_LENGTH) {
    errors.push({
      kind: "custom_prompt_too_long",
      length: p.customPrompt.length,
      max: CUSTOM_PROMPT_MAX_LENGTH,
    });
  }

  return errors;
}

export function profileFromRow(row: ProfileRow): Profile {
  return {
    persona: row.persona,
    language: row.language,
    lengthPref: row.length_pref,
    autonomyPref: row.autonomy_pref,
    gitPref: row.git_pref,
    codePref: row.code_pref,
    computerPref: row.computer_pref,
    processPref: row.process_pref,
    antiPatterns: row.anti_patterns,
    customPrompt: row.custom_prompt,
    promptMode: row.prompt_mode,
  };
}
