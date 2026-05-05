"use client";

// CEREBRO-PATCH(agent-profile-tab): cerebro modification of upstream file

import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2, Plus, RotateCcw, Save, X } from "lucide-react";
import { toast } from "sonner";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Slider } from "@multica/ui/components/ui/slider";
import { Input } from "@multica/ui/components/ui/input";
import { Button } from "@multica/ui/components/ui/button";
import { Label } from "@multica/ui/components/ui/label";
import { Badge } from "@multica/ui/components/ui/badge";
import { cn } from "@multica/ui/lib/utils";
import { useAuthStore } from "@multica/core/auth";
import {
  ANTI_PATTERN_MAX_LENGTH,
  ANTI_PATTERNS_MAX_COUNT,
  COMPILED_PROMPT_TOKEN_CAP,
  PERSONAS,
  PERSONA_PRESETS,
  buildDefaultProfile,
  compileProfile,
  estimateTokens,
  formatContextPercent,
  myProfileOptions,
  upsertMyProfileMutation,
  deleteMyProfileMutation,
  type Persona,
  type Profile,
  type Language,
} from "@multica/core/profile";
import type { UserProfileResponse } from "@multica/core/types";

function profileFromResponse(row: UserProfileResponse): Profile {
  return {
    persona: row.persona,
    language: row.language,
    lengthPref: row.length_pref,
    autonomyPref: row.autonomy_pref,
    techPref: row.tech_pref,
    antiPatterns: [...row.anti_patterns],
  };
}

function profileEquals(a: Profile, b: Profile): boolean {
  return (
    a.persona === b.persona &&
    a.language === b.language &&
    a.lengthPref === b.lengthPref &&
    a.autonomyPref === b.autonomyPref &&
    a.techPref === b.techPref &&
    a.antiPatterns.length === b.antiPatterns.length &&
    a.antiPatterns.every((p, i) => p === b.antiPatterns[i])
  );
}

// Slider value -> readable label, language-aware. Used for slider captions.
function sliderCaption(value: number, language: "da" | "en", axis: "length" | "ask" | "tech"): string {
  const lo = value <= 33;
  const hi = value > 66;
  if (language === "en") {
    if (axis === "length") return lo ? "Terse" : hi ? "Verbose" : "Balanced";
    if (axis === "ask") return lo ? "Cautious" : hi ? "Autonomous" : "Balanced";
    return lo ? "Surface" : hi ? "Deep" : "Mid";
  }
  if (axis === "length") return lo ? "Kort" : hi ? "Udførligt" : "Balanceret";
  if (axis === "ask") return lo ? "Forsigtig" : hi ? "Autonom" : "Balanceret";
  return lo ? "Overflade" : hi ? "Dybt" : "Mellem";
}

export function AgentProfileTab() {
  const user = useAuthStore((s) => s.user);
  const qc = useQueryClient();
  const profileQuery = useQuery(myProfileOptions());

  // Local edit state. Initialised from server data once it arrives; falls back
  // to the default profile until then. Re-syncs only on server-side changes
  // (compared via profileEquals) so local edits aren't clobbered by refetch.
  const [profile, setProfile] = useState<Profile>(() => buildDefaultProfile("grundig", "da"));
  const [savedProfile, setSavedProfile] = useState<Profile | null>(null);

  useEffect(() => {
    if (profileQuery.isLoading) return;
    const next = profileQuery.data ? profileFromResponse(profileQuery.data) : null;
    setSavedProfile(next);
    setProfile((current) => {
      if (next && !savedProfile) return next;
      if (next && savedProfile && profileEquals(savedProfile, current)) return next;
      return current;
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [profileQuery.data, profileQuery.isLoading]);

  const upsert = useMutation(upsertMyProfileMutation(qc));
  const reset = useMutation(deleteMyProfileMutation(qc));

  const compiled = useMemo(
    () => compileProfile(profile, { displayName: user?.name }),
    [profile, user?.name],
  );
  const estimate = useMemo(() => estimateTokens(compiled, profile.language), [compiled, profile.language]);
  const overCap = estimate.tokens > COMPILED_PROMPT_TOKEN_CAP;
  const t = profile.language;
  const dirty = savedProfile == null
    ? !profileEquals(profile, buildDefaultProfile("grundig", "da"))
    : !profileEquals(profile, savedProfile);

  const handleSave = () => {
    upsert.mutate(
      {
        persona: profile.persona,
        language: profile.language,
        length_pref: profile.lengthPref,
        autonomy_pref: profile.autonomyPref,
        tech_pref: profile.techPref,
        anti_patterns: profile.antiPatterns,
      },
      {
        onSuccess: () => {
          toast.success(t === "da" ? "Profil gemt" : "Profile saved");
        },
        onError: (err) => {
          toast.error(err instanceof Error ? err.message : t === "da" ? "Kunne ikke gemme profil" : "Failed to save profile");
        },
      },
    );
  };

  const handleReset = () => {
    reset.mutate(undefined, {
      onSuccess: () => {
        const fallback = buildDefaultProfile("grundig", "da");
        setProfile(fallback);
        setSavedProfile(null);
        toast.success(t === "da" ? "Profil nulstillet" : "Profile reset");
      },
      onError: (err) => {
        toast.error(err instanceof Error ? err.message : t === "da" ? "Kunne ikke nulstille" : "Failed to reset");
      },
    });
  };

  const handlePersona = (persona: Persona) => {
    const next = buildDefaultProfile(persona, profile.language);
    setProfile({ ...next, antiPatterns: [...next.antiPatterns] });
  };

  const handleSlider =
    (field: "lengthPref" | "autonomyPref" | "techPref") =>
    (value: number | readonly number[]) => {
      const v = Array.isArray(value) ? value[0] : (value as number);
      setProfile((p) => ({ ...p, [field]: v }));
    };

  const addAntiPattern = (raw: string) => {
    const value = raw.trim();
    if (!value) return;
    if (profile.antiPatterns.length >= ANTI_PATTERNS_MAX_COUNT) return;
    if (value.length > ANTI_PATTERN_MAX_LENGTH) return;
    if (profile.antiPatterns.includes(value)) return;
    setProfile((p) => ({ ...p, antiPatterns: [...p.antiPatterns, value] }));
  };

  const removeAntiPattern = (index: number) => {
    setProfile((p) => ({ ...p, antiPatterns: p.antiPatterns.filter((_, i) => i !== index) }));
  };

  const setLanguage = (lang: Language) => {
    setProfile((p) => ({ ...p, language: lang }));
  };

  return (
    <div className="space-y-8">
      <header className="space-y-1">
        <h2 className="text-sm font-semibold">{t === "da" ? "Agent-profil" : "Agent profile"}</h2>
        <p className="text-xs text-muted-foreground">
          {t === "da"
            ? "Indstil hvordan agenter skal kommunikere med dig. Profilen kompileres til en kompakt prompt der sendes med hvert agent-kald."
            : "Configure how agents communicate with you. Your profile compiles into a compact prompt sent with each agent call."}
        </p>
      </header>

      {/* Language toggle */}
      <section className="space-y-2">
        <Label className="text-xs text-muted-foreground">{t === "da" ? "Sprog" : "Language"}</Label>
        <div className="flex gap-2" role="radiogroup" aria-label={t === "da" ? "Sprog" : "Language"}>
          {(["da", "en"] as const).map((lang) => (
            <button
              key={lang}
              role="radio"
              aria-checked={profile.language === lang}
              onClick={() => setLanguage(lang)}
              className={cn(
                "rounded-md border px-3 py-1.5 text-xs transition-colors",
                profile.language === lang
                  ? "border-primary bg-primary/5 text-foreground"
                  : "border-border text-muted-foreground hover:text-foreground",
              )}
            >
              {lang === "da" ? "Dansk" : "English"}
            </button>
          ))}
        </div>
      </section>

      {/* 1. Persona */}
      <section className="space-y-3">
        <div className="flex items-baseline justify-between">
          <h3 className="text-sm font-medium">
            {t === "da" ? "1. Hvem ligner du?" : "1. Who are you?"}
          </h3>
          <span className="text-xs text-muted-foreground">
            {t === "da" ? "Vælger sliders for dig" : "Sets your sliders"}
          </span>
        </div>
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3" role="radiogroup">
          {PERSONAS.map((persona) => {
            const preset = PERSONA_PRESETS[persona];
            const active = profile.persona === persona;
            return (
              <button
                key={persona}
                role="radio"
                aria-checked={active}
                onClick={() => handlePersona(persona)}
                className={cn(
                  "rounded-lg border p-3 text-left transition-all",
                  active
                    ? "border-primary bg-primary/5 ring-1 ring-primary"
                    : "border-border hover:border-foreground/30",
                )}
              >
                <div className="text-sm font-medium">{preset.label[profile.language]}</div>
                <div className="mt-1 text-xs text-muted-foreground">{preset.blurb[profile.language]}</div>
              </button>
            );
          })}
        </div>
      </section>

      {/* 2-4. Sliders */}
      <section className="space-y-6">
        {(
          [
            {
              field: "lengthPref",
              axis: "length" as const,
              titleDa: "2. Hvor meget vil du læse?",
              titleEn: "2. How much do you want to read?",
            },
            {
              field: "autonomyPref",
              axis: "ask" as const,
              titleDa: "3. Hvor meget skal jeg spørge?",
              titleEn: "3. How much should I ask?",
            },
            {
              field: "techPref",
              axis: "tech" as const,
              titleDa: "4. Hvor teknisk?",
              titleEn: "4. How technical?",
            },
          ] as const
        ).map(({ field, axis, titleDa, titleEn }) => {
          const value = profile[field];
          return (
            <div key={field} className="space-y-2">
              <div className="flex items-baseline justify-between">
                <h3 className="text-sm font-medium">{t === "da" ? titleDa : titleEn}</h3>
                <span className="text-xs text-muted-foreground tabular-nums">
                  {sliderCaption(value, t, axis)} · {value}
                </span>
              </div>
              <Slider
                value={[value]}
                onValueChange={handleSlider(field)}
                min={0}
                max={100}
                step={1}
                aria-label={t === "da" ? titleDa : titleEn}
              />
            </div>
          );
        })}
      </section>

      {/* 5. Anti-patterns */}
      <section className="space-y-3">
        <div className="flex items-baseline justify-between">
          <h3 className="text-sm font-medium">
            {t === "da" ? "5. Hvad HADER du?" : "5. What do you HATE?"}
          </h3>
          <span className="text-xs text-muted-foreground">
            {profile.antiPatterns.length} / {ANTI_PATTERNS_MAX_COUNT}
          </span>
        </div>
        <AntiPatternEditor
          values={profile.antiPatterns}
          onAdd={addAntiPattern}
          onRemove={removeAntiPattern}
          language={profile.language}
        />
      </section>

      {/* Live preview */}
      <section className="space-y-3">
        <div className="flex items-baseline justify-between">
          <h3 className="text-sm font-medium">
            {t === "da" ? "Det her bliver sendt med hvert agent-kald" : "This is sent with every agent call"}
          </h3>
          <div className="flex items-center gap-2">
            <Badge variant={overCap ? "destructive" : "secondary"} className="tabular-nums">
              ~{estimate.tokens} {t === "da" ? "tokens" : "tokens"}
            </Badge>
            <span className="text-xs text-muted-foreground tabular-nums">
              {formatContextPercent(estimate.contextPercent)} {t === "da" ? "af 200k context" : "of 200k context"}
            </span>
          </div>
        </div>
        {overCap && (
          <p className="text-xs text-destructive">
            {t === "da"
              ? `Profilen er over loftet på ${COMPILED_PROMPT_TOKEN_CAP} tokens. Fjern et par anti-patterns for at fortsætte.`
              : `Profile exceeds the ${COMPILED_PROMPT_TOKEN_CAP}-token cap. Remove a few anti-patterns to continue.`}
          </p>
        )}
        <Card>
          <CardContent>
            <pre className="whitespace-pre-wrap font-mono text-xs leading-relaxed">{compiled}</pre>
          </CardContent>
        </Card>
        <p className="text-xs text-muted-foreground">
          {t === "da"
            ? "Token-tallet er et estimat (~±15%). Det faktiske antal bestemmes af modellens tokenizer ved kald-tid."
            : "Token count is an estimate (~±15%). The actual count is determined by the model's tokenizer at call time."}
        </p>
      </section>

      {/* Save / reset */}
      <div className="flex items-center justify-between pt-2">
        <div className="text-xs text-muted-foreground">
          {savedProfile
            ? (t === "da" ? "Profil er gemt og bruges på tværs af agent-kald." : "Profile is saved and used across agent calls.")
            : (t === "da" ? "Du har ikke gemt en profil endnu." : "You haven't saved a profile yet.")}
        </div>
        <div className="flex items-center gap-2">
          {savedProfile && (
            <Button
              size="sm"
              variant="ghost"
              onClick={handleReset}
              disabled={reset.isPending || upsert.isPending}
              title={t === "da" ? "Slet din gemte profil" : "Delete your saved profile"}
            >
              <RotateCcw className="h-3 w-3" />
              {t === "da" ? "Nulstil" : "Reset"}
            </Button>
          )}
          <Button
            size="sm"
            onClick={handleSave}
            disabled={!dirty || upsert.isPending || overCap}
          >
            {upsert.isPending ? <Loader2 className="h-3 w-3 animate-spin" /> : <Save className="h-3 w-3" />}
            {upsert.isPending
              ? (t === "da" ? "Gemmer..." : "Saving...")
              : (t === "da" ? "Gem profil" : "Save profile")}
          </Button>
        </div>
      </div>
    </div>
  );
}

function AntiPatternEditor({
  values,
  onAdd,
  onRemove,
  language,
}: {
  values: string[];
  onAdd: (value: string) => void;
  onRemove: (index: number) => void;
  language: "da" | "en";
}) {
  const [draft, setDraft] = useState("");
  const atCap = values.length >= ANTI_PATTERNS_MAX_COUNT;

  const submit = () => {
    onAdd(draft);
    setDraft("");
  };

  return (
    <div className="space-y-3">
      <div className="flex gap-2">
        <Input
          type="text"
          value={draft}
          maxLength={ANTI_PATTERN_MAX_LENGTH}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              submit();
            }
          }}
          placeholder={
            language === "da"
              ? "fx \"Let me know if you need anything else\""
              : "e.g. \"Let me know if you need anything else\""
          }
          disabled={atCap}
        />
        <Button size="sm" variant="secondary" onClick={submit} disabled={atCap || !draft.trim()}>
          <Plus className="h-3 w-3" />
          {language === "da" ? "Tilføj" : "Add"}
        </Button>
      </div>
      {values.length > 0 && (
        <ul className="flex flex-wrap gap-1.5">
          {values.map((value, index) => (
            <li key={`${index}-${value}`}>
              <button
                type="button"
                onClick={() => onRemove(index)}
                className="group inline-flex items-center gap-1 rounded-full border border-border bg-muted px-2.5 py-1 text-xs hover:border-destructive/50 hover:text-destructive"
                aria-label={language === "da" ? `Fjern: ${value}` : `Remove: ${value}`}
              >
                <span className="max-w-[20rem] truncate">{value}</span>
                <X className="h-3 w-3 opacity-50 group-hover:opacity-100" />
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
