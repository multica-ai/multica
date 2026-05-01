import { PERSONA_PRESETS } from "./presets";
import type { Language, Profile } from "./schema";

// Pure, deterministic Profile -> prompt-string transformation.
// No I/O, no clocks, no randomness — same input always produces same output,
// so the result can be safely cached and snapshot-tested.

interface SectionLabels {
  user: string;
  style: string;
  autonomy: string;
  tech: string;
  avoid: string;
  techArch: string;
  techData: string;
  techUx: string;
  techCode: string;
}

const LABELS: Record<Language, SectionLabels> = {
  da: {
    user: "USER",
    style: "STYLE",
    autonomy: "AUTONOMY",
    tech: "TECH",
    avoid: "AVOID",
    techArch: "arch",
    techData: "data",
    techUx: "ux",
    techCode: "code",
  },
  en: {
    user: "USER",
    style: "STYLE",
    autonomy: "AUTONOMY",
    tech: "TECH",
    avoid: "AVOID",
    techArch: "arch",
    techData: "data",
    techUx: "ux",
    techCode: "code",
  },
};

type TechDepth = "surface" | "medium" | "deep";

function bucketSlider(value: number): "low" | "mid" | "high" {
  if (value <= 33) return "low";
  if (value <= 66) return "mid";
  return "high";
}

function techDepth(value: number): TechDepth {
  if (value <= 33) return "surface";
  if (value <= 66) return "medium";
  return "deep";
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

// The 4 tech axes are derived from a single slider for v1. When we add
// per-axis controls in a later iteration, only this function needs to change.
function techLine(profile: Profile, labels: SectionLabels): string {
  const depth = techDepth(profile.techPref);
  return `${labels.techArch}:${depth}, ${labels.techData}:${depth}, ${labels.techUx}:${depth}, ${labels.techCode}:${depth}`;
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

  sections.push(`${labels.tech}: ${techLine(profile, labels)}`);

  if (profile.antiPatterns.length > 0) {
    sections.push("");
    sections.push(`${labels.avoid}:`);
    for (const pattern of profile.antiPatterns) sections.push(`- ${pattern}`);
  }

  return sections.join("\n");
}
