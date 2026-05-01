import type { Persona, Profile } from "./schema";

// Persona presets seed sliders + curate a small starter set of anti-patterns.
// Users may then tune any of the values without losing their persona choice.

export interface PersonaPreset {
  id: Persona;
  label: { da: string; en: string };
  blurb: { da: string; en: string };
  defaults: Omit<Profile, "persona" | "language">;
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
      antiPatterns: [
        "Let me know if you need anything else",
        "Great question!",
        "tids-estimater",
      ],
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
      antiPatterns: [
        "Forklar grundlæggende begreber",
        "Corporate-jargon",
      ],
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
      antiPatterns: [
        "Hop over edge-cases",
        "Antag uden at sige det",
      ],
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
      antiPatterns: [
        "Spring forklaring over",
        "Brug jargon uden at definere",
      ],
    },
  },
};

export const DEFAULT_PERSONA: Persona = "grundig";

export function buildDefaultProfile(persona: Persona = DEFAULT_PERSONA): Profile {
  const preset = PERSONA_PRESETS[persona];
  return {
    persona,
    language: "da",
    ...preset.defaults,
    antiPatterns: [...preset.defaults.antiPatterns],
  };
}
