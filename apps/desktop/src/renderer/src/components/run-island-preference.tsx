import { useEffect, useState } from "react";
import { Switch } from "@multica/ui/components/ui/switch";
import { Button } from "@multica/ui/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import {
  SettingsCard,
  SettingsRow,
  SettingsSection,
} from "@multica/views/settings";
import { useT } from "@multica/views/i18n";
import {
  islandChoices,
  type IslandDevices,
  type IslandProfile,
  type IslandRun,
  type IslandScreen,
} from "../../../shared/run-island";
import {
  changeIslandPreferences,
  setRunIslandEnabled,
  useIslandPreferences,
  useRunIslandEnabled,
} from "../platform/run-island-settings";
import { IslandCap, IslandRunList } from "./run-island-content";

function Choice<T extends string>({
  label,
  value,
  choices,
  select,
}: {
  label: string;
  value: T;
  choices: { value: T; label: string }[];
  select: (v: T) => void;
}) {
  return (
    <Select
      items={choices}
      value={value}
      onValueChange={(next) => {
        if (next !== null) select(next);
      }}
    >
      <SelectTrigger size="sm" className="w-full" aria-label={label}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent align="end">
        {choices.map((item) => (
          <SelectItem key={item.value} value={item.value}>
            {item.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

export function RunIslandPreference() {
  const { t } = useT("settings");
  const { t: issueText } = useT("issues");
  const enabled = useRunIslandEnabled();
  const prefs = useIslandPreferences();
  const [screenType, setScreenType] = useState<IslandScreen>("notch");
  const [devices, setDevices] = useState<IslandDevices>({
    sounds: [],
    displays: [],
  });
  const profile = prefs[screenType];
  const copy = t(($) => $.desktop.island, { returnObjects: true });
  const options = copy.options;
  useEffect(() => {
    if (!enabled) return;
    let mounted = true;
    const refresh = () => {
      void window.desktopAPI
        .runIslandDevices()
        .then((next) => {
          if (mounted) setDevices(next);
        })
        .catch(() => {});
    };
    refresh();
    const unsubscribe = window.desktopAPI.onRunIslandDisplaysChanged(refresh);
    window.addEventListener("focus", refresh);
    return () => {
      mounted = false;
      unsubscribe();
      window.removeEventListener("focus", refresh);
    };
  }, [enabled]);

  function profileChoice(key: keyof IslandProfile) {
    const choices = islandChoices[key].map((value) => ({
      value,
      label: options[value],
    }));
    return (
      <SettingsRow key={key} size="select" label={copy[key]}>
        <Choice
          label={copy[key]}
          value={profile[key]}
          choices={choices}
          select={(value) =>
            changeIslandPreferences((current) => ({
              ...current,
              [screenType]: { ...current[screenType], [key]: value },
            }))
          }
        />
      </SettingsRow>
    );
  }

  const samples: Pick<
    IslandRun,
    "name" | "detail" | "status" | "label" | "updated"
  >[] = [
    {
      name: "Nova",
      detail: copy.sampleReview,
      status: "running",
      label: issueText(($) => $.agent_activity.status_running),
      updated: 1,
    },
    {
      name: "Ada",
      detail: copy.sampleTest,
      status: "queued",
      label: issueText(($) => $.agent_activity.status_queued),
      updated: 2,
    },
  ];
  const previewRuns = samples.map((r) => ({
    ...r,
    id: r.name,
    agentId: r.name,
    audible: false,
    path: "",
  }));
  const displays = [
    { value: "auto", label: options.auto },
    ...devices.displays,
  ];
  if (!displays.some((d) => d.value === prefs.display))
    displays.push({ value: prefs.display, label: copy.unavailable });
  const selectedSound = devices.sounds.includes(prefs.sound) ? prefs.sound : "";

  return (
    <SettingsSection title={copy.title} scope="device">
      <SettingsCard>
        <SettingsRow label={copy.show} description={copy.description}>
          <Switch
            aria-label={copy.show}
            checked={enabled}
            onCheckedChange={setRunIslandEnabled}
          />
        </SettingsRow>
      </SettingsCard>
      {enabled && (
        <>
          <SettingsCard>
            <SettingsRow label={copy.soundEnabled} description={copy.soundHelp}>
              <Switch
                aria-label={copy.soundEnabled}
                checked={!prefs.muted}
                onCheckedChange={(v) =>
                  changeIslandPreferences((p) => ({ ...p, muted: !v }))
                }
              />
            </SettingsRow>
            {devices.sounds.length > 0 && (
              <SettingsRow label={copy.sound} size="select">
                <div className="flex items-center gap-2">
                  <Choice
                    label={copy.sound}
                    value={selectedSound}
                    choices={devices.sounds.map((v) => ({
                      value: v,
                      label: v,
                    }))}
                    select={(sound) => {
                      changeIslandPreferences((p) => ({ ...p, sound }));
                      window.desktopAPI.previewRunIslandSound(sound);
                    }}
                  />
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={!selectedSound}
                    onClick={() =>
                      window.desktopAPI.previewRunIslandSound(selectedSound)
                    }
                  >
                    {copy.previewSound}
                  </Button>
                </div>
              </SettingsRow>
            )}
            <SettingsRow label={copy.display} size="select">
              <Choice
                label={copy.display}
                value={prefs.display}
                choices={displays}
                select={(display) =>
                  changeIslandPreferences((p) => ({ ...p, display }))
                }
              />
            </SettingsRow>
          </SettingsCard>
          <SettingsCard>
            <SettingsRow label={copy.profile} size="select">
              <Choice
                label={copy.profile}
                value={screenType}
                choices={[
                  { value: "notch", label: options.notch },
                  { value: "floating", label: options.floating },
                ]}
                select={setScreenType}
              />
            </SettingsRow>
            <figure
              aria-label={copy.preview}
              className="m-3 rounded-lg bg-muted p-3"
            >
              <figcaption className="mb-2 text-caption text-muted-foreground">
                {copy.preview}
              </figcaption>
              <div
                className="island-surface mx-auto max-w-80 p-1"
                data-background={profile.background}
                data-attached={screenType === "notch"}
              >
                <div className="cap h-8">
                  <IslandCap
                    runs={previewRuns}
                    profile={profile}
                    attached={screenType === "notch"}
                  />
                </div>
                <IslandRunList runs={previewRuns} profile={profile} />
              </div>
            </figure>
            {profileChoice("background")}
            {profileChoice("trailing")}
            {screenType === "floating" && profileChoice("center")}
            {profileChoice("indicator")}
            {profileChoice("group")}
            {profileChoice("sort")}
          </SettingsCard>
        </>
      )}
    </SettingsSection>
  );
}
