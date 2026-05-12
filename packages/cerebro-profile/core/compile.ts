// CEREBRO-PATCH(core-profile-compile): cerebro modification of upstream file
import { PERSONA_PRESETS } from "./presets";
import { SCOPE_MAX, SCOPE_MIN, type Language, type Profile } from "./schema";

// Pure, deterministic Profile -> prompt-string transformation.
// No I/O, no clocks, no randomness — same input always produces same output,
// so the result can be safely cached and snapshot-tested.

interface SectionLabels {
  user: string;
  style: string;
  autonomy: string;
  scope: string;
  avoid: string;
  custom: string;
  scopeGit: string;
  scopeCode: string;
  scopeComputer: string;
  scopeProcess: string;
}

const LABELS: Record<Language, SectionLabels> = {
  da: {
    user: "USER",
    style: "STYLE",
    autonomy: "AUTONOMY",
    scope: "SCOPE",
    avoid: "AVOID",
    custom: "CUSTOM",
    scopeGit: "git",
    scopeCode: "code",
    scopeComputer: "computer",
    scopeProcess: "process",
  },
  en: {
    user: "USER",
    style: "STYLE",
    autonomy: "AUTONOMY",
    scope: "SCOPE",
    avoid: "AVOID",
    custom: "CUSTOM",
    scopeGit: "git",
    scopeCode: "code",
    scopeComputer: "computer",
    scopeProcess: "process",
  },
};

function bucketSlider(value: number): "low" | "mid" | "high" {
  if (value <= 33) return "low";
  if (value <= 66) return "mid";
  return "high";
}

function clampScope(value: number): number {
  if (value < SCOPE_MIN) return SCOPE_MIN;
  if (value > SCOPE_MAX) return SCOPE_MAX;
  return Math.round(value);
}

function styleLines(profile: Profile): string[] {
  const length = bucketSlider(profile.lengthPref);
  if (profile.language === "en") {
    if (length === "low") {
      return [
        "Default 1–3 sentences. Use a table/list for >5 items.",
        "Skip greetings and \"let me know\"-closings.",
        "Match names from the code exactly. No invented terms.",
      ];
    }
    if (length === "mid") {
      return [
        "Default 3–6 sentences. Add structure for >5 items.",
        "Match names from the code exactly. No invented terms.",
      ];
    }
    return [
      "Explain reasoning and trade-offs. Use sections for longer answers.",
      "Match names from the code exactly. No invented terms.",
    ];
  }

  if (length === "low") {
    return [
      "Default 1–3 sætninger. Vis data i tabel/liste hvis >5 punkter.",
      "Spring hilsner og \"let me know\"-closings over.",
      "Match navne fra koden eksakt. Ingen opfundne ord.",
    ];
  }
  if (length === "mid") {
    return [
      "Default 3–6 sætninger. Brug struktur ved >5 punkter.",
      "Match navne fra koden eksakt. Ingen opfundne ord.",
    ];
  }
  return [
    "Forklar ræsonnement og trade-offs. Brug sektioner ved længere svar.",
    "Match navne fra koden eksakt. Ingen opfundne ord.",
  ];
}

function autonomyLines(profile: Profile): string[] {
  const autonomy = bucketSlider(profile.autonomyPref);
  if (profile.language === "en") {
    if (autonomy === "high") {
      return [
        "Reversible: just do it, show what happened.",
        "Destructive: ask first.",
        "Don't ask about things you can find or do yourself.",
      ];
    }
    if (autonomy === "mid") {
      return [
        "Reversible: do it, summarise the change.",
        "Destructive or ambiguous: confirm first.",
      ];
    }
    return [
      "Confirm plan before non-trivial changes.",
      "Destructive: always ask first.",
    ];
  }

  if (autonomy === "high") {
    return [
      "Reversibelt: bare kør, vis hvad der skete.",
      "Destruktivt: spørg først.",
      "Spørg ikke om ting du selv kan finde/gøre.",
    ];
  }
  if (autonomy === "mid") {
    return [
      "Reversibelt: gør det, opsummér ændringen.",
      "Destruktivt eller tvetydigt: bekræft først.",
    ];
  }
  return [
    "Bekræft plan før ikke-trivielle ændringer.",
    "Destruktivt: spørg altid først.",
  ];
}

// Renders the four self-rated scope dimensions on one line. Each value is
// clamped to 1-5; the validator at the API boundary already enforces the
// range, but the clamp protects the prompt format against any bypass.
function scopeLine(profile: Profile, labels: SectionLabels): string {
  return (
    `${labels.scopeGit}:${clampScope(profile.gitPref)}, ` +
    `${labels.scopeCode}:${clampScope(profile.codePref)}, ` +
    `${labels.scopeComputer}:${clampScope(profile.computerPref)}, ` +
    `${labels.scopeProcess}:${clampScope(profile.processPref)}`
  );
}

function userHeader(profile: Profile): string {
  const preset = PERSONA_PRESETS[profile.persona];
  const personaLabel = preset.label[profile.language];
  const langWord = profile.language === "da" ? "dansk" : "english";
  return `${personaLabel}, ${langWord}`;
}

export interface CompileOptions {
  // Prepend a known display name. Compile stays pure: caller supplies the name.
  displayName?: string;
}

export function compileProfile(profile: Profile, options: CompileOptions = {}): string {
  const labels = LABELS[profile.language];
  const custom = profile.customPrompt.trim();

  // Replace mode + non-empty custom prompt → the user's own prompt fully
  // overrides the compiled output. Empty custom prompt falls back to the
  // structured prompt so an agent never sees an empty profile.
  if (profile.promptMode === "replace" && custom.length > 0) {
    return custom;
  }

  const header = options.displayName
    ? `${labels.user}: ${options.displayName} (${userHeader(profile)})`
    : `${labels.user}: ${userHeader(profile)}`;

  const sections: string[] = [header, ""];

  sections.push(`${labels.style}:`);
  for (const line of styleLines(profile)) sections.push(`- ${line}`);
  sections.push("");

  sections.push(`${labels.autonomy}:`);
  for (const line of autonomyLines(profile)) sections.push(`- ${line}`);
  sections.push("");

  sections.push(`${labels.scope}: ${scopeLine(profile, labels)}`);

  if (profile.antiPatterns.length > 0) {
    sections.push("");
    sections.push(`${labels.avoid}:`);
    for (const pattern of profile.antiPatterns) sections.push(`- ${pattern}`);
  }

  // Append mode pastes the custom prompt under its own CUSTOM: section,
  // preserving the structured prompt above it.
  if (custom.length > 0) {
    sections.push("");
    sections.push(`${labels.custom}:`);
    sections.push(custom);
  }

  return sections.join("\n");
}
