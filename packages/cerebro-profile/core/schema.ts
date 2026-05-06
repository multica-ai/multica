// CEREBRO-PATCH(core-profile-schema): cerebro modification of upstream file
// User communication profile (JEH-304). Mirrors the user_profile table.

export const PERSONAS = ["utalmodig", "ekspert", "grundig", "larling"] as const;
export type Persona = (typeof PERSONAS)[number];

export const LANGUAGES = ["da", "en"] as const;
export type Language = (typeof LANGUAGES)[number];

export const SLIDER_MIN = 0;
export const SLIDER_MAX = 100;

export const ANTI_PATTERNS_MAX_COUNT = 20;
export const ANTI_PATTERN_MAX_LENGTH = 100;

// Hard cap on compiled prompt size. Enforced server-side; UI shows a warning
// before the user hits it. Aligned with the issue's <200 token target.
export const COMPILED_PROMPT_TOKEN_CAP = 200;

export interface Profile {
  persona: Persona;
  language: Language;
  lengthPref: number;
  autonomyPref: number;
  techPref: number;
  antiPatterns: string[];
}

export interface ProfileRow {
  user_id: string;
  persona: Persona;
  language: Language;
  length_pref: number;
  autonomy_pref: number;
  tech_pref: number;
  anti_patterns: string[];
  updated_at: string;
}

export type ProfileValidationError =
  | { kind: "invalid_persona"; got: string }
  | { kind: "invalid_language"; got: string }
  | { kind: "slider_out_of_range"; field: keyof Profile; got: number }
  | { kind: "too_many_anti_patterns"; count: number; max: number }
  | { kind: "anti_pattern_too_long"; index: number; length: number; max: number };

export function validateProfile(p: Profile): ProfileValidationError[] {
  const errors: ProfileValidationError[] = [];

  if (!PERSONAS.includes(p.persona)) {
    errors.push({ kind: "invalid_persona", got: p.persona });
  }
  if (!LANGUAGES.includes(p.language)) {
    errors.push({ kind: "invalid_language", got: p.language });
  }

  for (const field of ["lengthPref", "autonomyPref", "techPref"] as const) {
    const value = p[field];
    if (!Number.isInteger(value) || value < SLIDER_MIN || value > SLIDER_MAX) {
      errors.push({ kind: "slider_out_of_range", field, got: value });
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

  return errors;
}

export function profileFromRow(row: ProfileRow): Profile {
  return {
    persona: row.persona,
    language: row.language,
    lengthPref: row.length_pref,
    autonomyPref: row.autonomy_pref,
    techPref: row.tech_pref,
    antiPatterns: row.anti_patterns,
  };
}
