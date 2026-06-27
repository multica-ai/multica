"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { notificationPreferenceOptions } from "@multica/core/notification-preferences/queries";
import { useUpdateNotificationPreferences } from "@multica/core/notification-preferences/mutations";
import type { NotificationPreferences } from "@multica/core/types";
import { soundManager, type SoundType } from "@multica/core/sound";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Switch } from "@multica/ui/components/ui/switch";
import { Slider } from "@multica/ui/components/ui/slider";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Button } from "@multica/ui/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import { toast } from "sonner";
import { Volume2Icon } from "lucide-react";
import { useT } from "../../i18n";

type SceneI18nKey = "issue_done" | "issue_blocked" | "child_blocked" | "in_review" | "mention_decision" | "task_failed";

/** Per-scene sound toggle definition. */
interface SoundScene {
  key: keyof NotificationPreferences;
  sound: SoundType;
  i18nKey: SceneI18nKey;
}

const SOUND_SCENES: SoundScene[] = [
  { key: "sound_issue_done", sound: "complete", i18nKey: "issue_done" },
  { key: "sound_blocked", sound: "blocked", i18nKey: "issue_blocked" },
  { key: "sound_child_blocked", sound: "blocked", i18nKey: "child_blocked" },
  { key: "sound_in_review", sound: "action_required", i18nKey: "in_review" },
  { key: "sound_mention_decision", sound: "action_required", i18nKey: "mention_decision" },
  { key: "sound_task_failed", sound: "attention", i18nKey: "task_failed" },
];

export function SoundSettings() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const { data } = useQuery(notificationPreferenceOptions(wsId));
  const mutation = useUpdateNotificationPreferences();

  const preferences = data?.preferences ?? {};

  const handleToggle = (key: keyof NotificationPreferences, enabled: boolean) => {
    const updated: NotificationPreferences = {
      ...preferences,
      [key]: enabled ? "all" : "muted",
    };
    if (enabled) {
      delete updated[key];
    }
    mutation.mutate(updated, {
      onError: (err) =>
        toast.error(
          err instanceof Error && err.message
            ? err.message
            : t(($) => $.notifications.toast_failed),
        ),
    });
  };

  const handleVolumeChange = (value: number | readonly number[]) => {
    const v = Array.isArray(value) ? value[0] : value;
    if (v === undefined) return;
    const updated: NotificationPreferences = {
      ...preferences,
      sound_volume: v,
    };
    mutation.mutate(updated, {
      onError: (err) =>
        toast.error(
          err instanceof Error && err.message
            ? err.message
            : t(($) => $.notifications.toast_failed),
        ),
    });
  };

  const handleThemeChange = (theme: string | null) => {
    if (theme === null) return;
    const updated: NotificationPreferences = {
      ...preferences,
      sound_theme: theme,
    };
    if (theme === "default") {
      delete updated.sound_theme;
    }
    mutation.mutate(updated, {
      onError: (err) =>
        toast.error(
          err instanceof Error && err.message
            ? err.message
            : t(($) => $.notifications.toast_failed),
        ),
    });
  };

  const handlePreview = (sound: SoundType) => {
    soundManager.init();
    soundManager.play(sound, 70, "default");
  };

  const soundEnabled = preferences.sound_enabled !== "muted";
  const volume = preferences.sound_volume ?? 70;
  const theme = preferences.sound_theme ?? "default";

  return (
    <section className="space-y-4">
      <div>
        <h2 className="text-sm font-semibold">{t(($) => $.notifications.sound.title)}</h2>
        <p className="text-sm text-muted-foreground mt-1">
          {t(($) => $.notifications.sound.description)}
        </p>
      </div>

      <Card>
        <CardContent className="divide-y">
          {/* Global sound toggle */}
          <div className="flex items-center justify-between py-3 first:pt-0">
            <div className="space-y-0.5 pr-4">
              <p className="text-sm font-medium">{t(($) => $.notifications.sound.global_toggle)}</p>
            </div>
            <Switch
              checked={soundEnabled}
              onCheckedChange={(checked) => handleToggle("sound_enabled", checked)}
            />
          </div>

          {/* Volume slider */}
          <div className="flex items-center justify-between py-3">
            <div className="space-y-0.5 pr-4 flex-1">
              <p className="text-sm font-medium">{t(($) => $.notifications.sound.volume)}</p>
            </div>
            <div className="flex items-center gap-3 min-w-[180px]">
              <Volume2Icon className="size-4 text-muted-foreground shrink-0" />
              <Slider
                value={[volume]}
                onValueChange={handleVolumeChange}
                min={0}
                max={100}
                className="flex-1"
                aria-label={t(($) => $.notifications.sound.volume)}
              />
              <span className="text-xs text-muted-foreground w-8 text-right tabular-nums">
                {volume}%
              </span>
            </div>
          </div>

          {/* Sound theme */}
          <div className="flex items-center justify-between py-3">
            <div className="space-y-0.5 pr-4">
              <p className="text-sm font-medium">{t(($) => $.notifications.sound.theme)}</p>
            </div>
            <Select value={theme} onValueChange={handleThemeChange}>
              <SelectTrigger size="sm" className="w-[140px]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="default">{t(($) => $.notifications.sound.theme_default)}</SelectItem>
                <SelectItem value="soft">{t(($) => $.notifications.sound.theme_soft)}</SelectItem>
                <SelectItem value="alert">{t(($) => $.notifications.sound.theme_alert)}</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </CardContent>
      </Card>

      {/* Per-scene toggles */}
      <div>
        <p className="text-sm text-muted-foreground mb-3">
          {t(($) => $.notifications.sound.scenes_description)}
        </p>
      </div>

      <Card>
        <CardContent className="divide-y">
          {SOUND_SCENES.map((scene) => {
            const enabled = preferences[scene.key] !== "muted";
            return (
              <div
                key={scene.key}
                className="flex items-center justify-between py-3 first:pt-0 last:pb-0"
              >
                <div className="space-y-0.5 pr-4">
                  <p className="text-sm font-medium">
                    {t(($) => $.notifications.sound.scenes[scene.i18nKey].label)}
                  </p>
                  <p className="text-xs text-muted-foreground">
                    {t(($) => $.notifications.sound.scenes[scene.i18nKey].description)}
                  </p>
                </div>
                <div className="flex items-center gap-2 shrink-0">
                  <Tooltip>
                    <TooltipTrigger>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="size-8"
                        aria-label={t(($) => $.notifications.sound.preview)}
                        onClick={() => handlePreview(scene.sound)}
                      >
                        <Volume2Icon className="size-4" />
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>
                      {t(($) => $.notifications.sound.preview)}
                    </TooltipContent>
                  </Tooltip>
                  <Switch
                    checked={enabled}
                    onCheckedChange={(checked) => handleToggle(scene.key, checked)}
                  />
                </div>
              </div>
            );
          })}

          {/* PR reviewer status — disabled until GitHub integration is ready */}
          <div className="flex items-center justify-between py-3 first:pt-0 last:pb-0">
            <div className="space-y-0.5 pr-4">
              <p className="text-sm font-medium text-muted-foreground">
                {t(($) => $.notifications.sound.scenes.pr_reviewer.label)}
              </p>
              <p className="text-xs text-muted-foreground">
                {t(($) => $.notifications.sound.scenes.pr_reviewer.description)}
              </p>
            </div>
            <div className="flex items-center gap-2 shrink-0">
              <Tooltip>
                <TooltipTrigger>
                  <span className="inline-flex">
                    <Switch
                      disabled
                      checked={false}
                      aria-label={t(($) => $.notifications.sound.scenes.pr_reviewer.label)}
                    />
                  </span>
                </TooltipTrigger>
                <TooltipContent>
                  {t(($) => $.notifications.sound.pr_reviewer_disabled_tooltip)}
                </TooltipContent>
              </Tooltip>
            </div>
          </div>
        </CardContent>
      </Card>
    </section>
  );
}
