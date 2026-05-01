import type { Language, Persona, Profile } from "./schema";

// Persona presets seed sliders + curate a small starter set of anti-patterns.
// Users may then tune any of the values without losing their persona choice.

export interface LocalizedAntiPatterns {
  da: string[];
  en: string[];
}

export interface PersonaPreset {
  id: Persona;
  label: { da: string; en: string };
  blurb: { da: string; en: string };
  defaults: {
    lengthPref: number;
    autonomyPref: number;
    techPref: number;
    antiPatterns: LocalizedAntiPatterns;
  };
}

export const PERSONA_PRESETS: Record<Persona, PersonaPreset> = {
  utalmodig: {
    id: "utalmodig",
    label: { da: "Den utålmodige", en: "The impatient" },
    blurb: {
      da: "Korte svar, ingen pjat. Bare gør det.",
      en: "Short answers, no filler. Just do it.",
    },
    defaults: {
      lengthPref: 15,
      autonomyPref: 85,
      techPref: 70,
      antiPatterns: {
        da: [
          "\"Let me know if you need anything else\"",
          "\"Great question!\"",
          "Tids-estimater",
        ],
        en: [
          "\"Let me know if you need anything else\"",
          "\"Great question!\"",
          "Time estimates",
        ],
      },
    },
  },
  ekspert: {
    id: "ekspert",
    label: { da: "Eksperten", en: "The expert" },
    blurb: {
      da: "Spring grundlaget over. Tal til mig som en kollega.",
      en: "Skip the basics. Talk to me like a peer.",
    },
    defaults: {
      lengthPref: 40,
      autonomyPref: 70,
      techPref: 95,
      antiPatterns: {
        da: ["Forklar grundlæggende begreber", "Corporate-jargon"],
        en: ["Explaining basic concepts", "Corporate jargon"],
      },
    },
  },
  grundig: {
    id: "grundig",
    label: { da: "Den grundige", en: "The thorough" },
    blurb: {
      da: "Vis trade-offs og alternativer. Spørg før destruktivt.",
      en: "Show trade-offs and alternatives. Ask before destructive ops.",
    },
    defaults: {
      lengthPref: 75,
      autonomyPref: 30,
      techPref: 80,
      antiPatterns: {
        da: ["Hop over edge-cases", "Antag uden at sige det"],
        en: ["Skipping edge cases", "Unstated assumptions"],
      },
    },
  },
  larling: {
    id: "larling",
    label: { da: "Lærlingen", en: "The apprentice" },
    blurb: {
      da: "Forklar undervejs. Hjælp mig med at lære.",
      en: "Explain as you go. Help me learn.",
    },
    defaults: {
      lengthPref: 70,
      autonomyPref: 25,
      techPref: 35,
      antiPatterns: {
        da: ["Spring forklaring over", "Brug jargon uden at definere"],
        en: ["Skipping explanation", "Using jargon without defining it"],
      },
    },
  },
};

export const DEFAULT_PERSONA: Persona = "grundig";
export const DEFAULT_LANGUAGE: Language = "da";

export function buildDefaultProfile(
  persona: Persona = DEFAULT_PERSONA,
  language: Language = DEFAULT_LANGUAGE,
): Profile {
  const preset = PERSONA_PRESETS[persona];
  return {
    persona,
    language,
    lengthPref: preset.defaults.lengthPref,
    autonomyPref: preset.defaults.autonomyPref,
    techPref: preset.defaults.techPref,
    antiPatterns: [...preset.defaults.antiPatterns[language]],
  };
}
