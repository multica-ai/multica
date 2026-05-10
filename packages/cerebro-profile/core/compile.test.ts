import { describe, expect, it } from "vitest";
import { compileProfile } from "./compile";
import { PERSONAS, type Profile, validateProfile } from "./schema";
import { buildDefaultProfile } from "./presets";

describe("compileProfile", () => {
  it("is deterministic — same input produces identical output", () => {
    const profile = buildDefaultProfile("utalmodig");
    const a = compileProfile(profile);
    const b = compileProfile(profile);
    expect(a).toBe(b);
  });

  it("does not mutate input", () => {
    const profile = buildDefaultProfile("ekspert");
    const before = JSON.stringify(profile);
    compileProfile(profile, { displayName: "Jens" });
    expect(JSON.stringify(profile)).toBe(before);
  });

  for (const persona of PERSONAS) {
    it(`snapshot — persona "${persona}" (default profile)`, () => {
      const profile = buildDefaultProfile(persona);
      const compiled = compileProfile(profile, { displayName: "Jens" });
      expect(compiled).toBe(defaultProfileOutputs[persona]);
    });
  }

  it("snapshot — utalmodig in english renders english labels + english anti-patterns", () => {
    const profile = buildDefaultProfile("utalmodig", "en");
    expect(compileProfile(profile, { displayName: "Jens" })).toBe(englishUtålmodigOutput);
  });

  it("omits AVOID section when there are no anti-patterns", () => {
    const profile: Profile = { ...buildDefaultProfile("ekspert"), antiPatterns: [] };
    const compiled = compileProfile(profile);
    expect(compiled).not.toContain("AVOID");
  });

  it("renders header without display name when none provided", () => {
    const profile = buildDefaultProfile("grundig");
    const compiled = compileProfile(profile);
    expect(compiled.startsWith("USER: Den grundige")).toBe(true);
  });

  it("low-end sliders produce terse style + high-autonomy line set", () => {
    const profile: Profile = {
      ...buildDefaultProfile("utalmodig"),
      lengthPref: 0,
      autonomyPref: 100,
      techPref: 0,
    };
    const compiled = compileProfile(profile);
    expect(compiled).toContain("1–3 sætninger");
    expect(compiled).toContain("Reversibelt: bare kør");
    expect(compiled).toContain("arch:surface");
  });

  it("max sliders produce verbose style + cautious autonomy + deep tech", () => {
    const profile: Profile = {
      ...buildDefaultProfile("grundig"),
      lengthPref: 100,
      autonomyPref: 0,
      techPref: 100,
    };
    const compiled = compileProfile(profile);
    expect(compiled).toContain("Forklar ræsonnement");
    expect(compiled).toContain("Bekræft plan");
    expect(compiled).toContain("arch:deep");
  });

  it("snapshot — empty anti-patterns + mid-range sliders", () => {
    const profile: Profile = {
      persona: "ekspert",
      language: "da",
      lengthPref: 50,
      autonomyPref: 50,
      techPref: 50,
      antiPatterns: [],
    };
    expect(compileProfile(profile, { displayName: "Jens" })).toBe(emptyAntiPatternsOutput);
  });

  it("snapshot — max anti-patterns at boundary", () => {
    const profile: Profile = {
      ...buildDefaultProfile("utalmodig"),
      antiPatterns: Array.from({ length: 20 }, (_, i) => `anti-pattern-${i + 1}`),
    };
    expect(validateProfile(profile)).toEqual([]);
    expect(compileProfile(profile, { displayName: "Jens" })).toBe(maxAntiPatternsOutput);
  });
});

const defaultProfileOutputs: Record<(typeof PERSONAS)[number], string> = {
  ekspert: `USER: Jens (Eksperten, dansk)

STYLE:
- Default 3–6 sætninger. Brug struktur ved >5 punkter.
- Match navne fra koden eksakt. Ingen opfundne ord.

AUTONOMY:
- Reversibelt: bare kør, vis hvad der skete.
- Destruktivt: spørg først.
- Spørg ikke om ting du selv kan finde/gøre.

TECH: arch:deep, data:deep, ux:deep, code:deep

AVOID:
- Forklar grundlæggende begreber
- Corporate-jargon`,
  grundig: `USER: Jens (Den grundige, dansk)

STYLE:
- Forklar ræsonnement og trade-offs. Brug sektioner ved længere svar.
- Match navne fra koden eksakt. Ingen opfundne ord.

AUTONOMY:
- Bekræft plan før ikke-trivielle ændringer.
- Destruktivt: spørg altid først.

TECH: arch:deep, data:deep, ux:deep, code:deep

AVOID:
- Hop over edge-cases
- Antag uden at sige det`,
  larling: `USER: Jens (Lærlingen, dansk)

STYLE:
- Forklar ræsonnement og trade-offs. Brug sektioner ved længere svar.
- Match navne fra koden eksakt. Ingen opfundne ord.

AUTONOMY:
- Bekræft plan før ikke-trivielle ændringer.
- Destruktivt: spørg altid først.

TECH: arch:medium, data:medium, ux:medium, code:medium

AVOID:
- Spring forklaring over
- Brug jargon uden at definere`,
  utalmodig: `USER: Jens (Den utålmodige, dansk)

STYLE:
- Default 1–3 sætninger. Vis data i tabel/liste hvis >5 punkter.
- Spring hilsner og "let me know"-closings over.
- Match navne fra koden eksakt. Ingen opfundne ord.

AUTONOMY:
- Reversibelt: bare kør, vis hvad der skete.
- Destruktivt: spørg først.
- Spørg ikke om ting du selv kan finde/gøre.

TECH: arch:deep, data:deep, ux:deep, code:deep

AVOID:
- "Let me know if you need anything else"
- "Great question!"
- Tids-estimater`,
};

const englishUtålmodigOutput = `USER: Jens (The impatient, english)

STYLE:
- Default 1–3 sentences. Use a table/list for >5 items.
- Skip greetings and "let me know"-closings.
- Match names from the code exactly. No invented terms.

AUTONOMY:
- Reversible: just do it, show what happened.
- Destructive: ask first.
- Don't ask about things you can find or do yourself.

TECH: arch:deep, data:deep, ux:deep, code:deep

AVOID:
- "Let me know if you need anything else"
- "Great question!"
- Time estimates`;

const emptyAntiPatternsOutput = `USER: Jens (Eksperten, dansk)

STYLE:
- Default 3–6 sætninger. Brug struktur ved >5 punkter.
- Match navne fra koden eksakt. Ingen opfundne ord.

AUTONOMY:
- Reversibelt: gør det, opsummér ændringen.
- Destruktivt eller tvetydigt: bekræft først.

TECH: arch:medium, data:medium, ux:medium, code:medium`;

const maxAntiPatternsOutput = `USER: Jens (Den utålmodige, dansk)

STYLE:
- Default 1–3 sætninger. Vis data i tabel/liste hvis >5 punkter.
- Spring hilsner og "let me know"-closings over.
- Match navne fra koden eksakt. Ingen opfundne ord.

AUTONOMY:
- Reversibelt: bare kør, vis hvad der skete.
- Destruktivt: spørg først.
- Spørg ikke om ting du selv kan finde/gøre.

TECH: arch:deep, data:deep, ux:deep, code:deep

AVOID:
- anti-pattern-1
- anti-pattern-2
- anti-pattern-3
- anti-pattern-4
- anti-pattern-5
- anti-pattern-6
- anti-pattern-7
- anti-pattern-8
- anti-pattern-9
- anti-pattern-10
- anti-pattern-11
- anti-pattern-12
- anti-pattern-13
- anti-pattern-14
- anti-pattern-15
- anti-pattern-16
- anti-pattern-17
- anti-pattern-18
- anti-pattern-19
- anti-pattern-20`;
