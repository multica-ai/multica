"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { Copy, Pause, Play, RotateCcw, Trash2 } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { ShimmerText } from "@multica/ui/components/common/shimmer-text";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { copyText } from "@multica/ui/lib/clipboard";
import { cn } from "@multica/ui/lib/utils";
import {
  pickVoiceModel,
  transcribeVoiceClip,
  useVoiceSettingsStore,
  VOICE_HISTORY_RETENTIONS,
  voiceHistoryDays,
  type VoiceHistoryEntry,
  type VoiceHistoryRetention,
} from "@multica/core/voice";
import { useLocale, useT } from "../../i18n";
import { localVoiceAPI, type LocalVoiceHistoryAPI } from "../../platform/local-voice";
import { useVoiceModels } from "../../voice";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";

const PAGE = 50;

function useHistory(api: LocalVoiceHistoryAPI | undefined): VoiceHistoryEntry[] | null {
  const [entries, setEntries] = useState<VoiceHistoryEntry[] | null>(null);
  useEffect(() => {
    if (!api) return;
    let live = true;
    const load = () =>
      void api.list().then(
        (next) => live && setEntries(next),
        () => live && setEntries([]),
      );
    load();
    const unsubscribe = api.onChanged(load);
    return () => {
      live = false;
      unsubscribe();
    };
  }, [api]);
  return entries;
}

/** One recording at a time, from its kept audio. */
function usePlayer(api: LocalVoiceHistoryAPI | undefined) {
  const [playing, setPlaying] = useState<string | null>(null);
  const current = useRef<{ audio: HTMLAudioElement; url: string } | null>(null);
  const release = () => {
    if (!current.current) return;
    current.current.audio.pause();
    URL.revokeObjectURL(current.current.url);
    current.current = null;
  };
  const stop = () => {
    release();
    setPlaying(null);
  };
  useEffect(() => release, []);
  const toggle = async (id: string) => {
    const wasPlaying = playing === id;
    stop();
    if (wasPlaying || !api) return;
    const kept = await api.audio(id);
    if (!kept) return;
    const url = URL.createObjectURL(new Blob([kept.audio], { type: kept.mimeType }));
    const audio = new Audio(url);
    current.current = { audio, url };
    audio.onended = stop;
    setPlaying(id);
    await audio.play().catch(stop);
  };
  return { playing, toggle };
}

function dayKey(time: number): string {
  const date = new Date(time);
  return `${date.getFullYear()}-${date.getMonth()}-${date.getDate()}`;
}

function formatDuration(ms: number): string {
  const seconds = Math.max(1, Math.round(ms / 1000));
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`;
}

/**
 * Dictations kept on this computer, newest first and grouped by day. Each
 * one can be played, transcribed again from its audio, copied or deleted.
 */
export function VoiceHistorySection() {
  const { t } = useT("settings");
  const api = useMemo(() => localVoiceAPI()?.history, []);
  const entries = useHistory(api);
  const keepHistory = useVoiceSettingsStore((state) => state.keepHistory);
  const [confirmClear, setConfirmClear] = useState(false);
  if (!api) return null;
  return (
    <SettingsSection
      title={t(($) => $.voice_input.history.title)}
      description={t(($) => $.voice_input.history.description)}
      anchor="voice-history"
      action={
        entries && entries.length > 0 ? (
          <Button size="xs" variant="outline" onClick={() => setConfirmClear(true)}>
            {t(($) => $.voice_input.history.clear)}
          </Button>
        ) : undefined
      }
    >
      <SettingsCard>
        <KeepHistoryRow api={api} />
      </SettingsCard>
      {entries && entries.length > 0 ? (
        <HistoryList api={api} entries={entries} />
      ) : entries ? (
        <p className="px-0.5 text-caption text-muted-foreground">
          {keepHistory === "off" ? t(($) => $.voice_input.history.off) : t(($) => $.voice_input.history.empty)}
        </p>
      ) : null}
      <AlertDialog open={confirmClear} onOpenChange={setConfirmClear}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.voice_input.history.clear_dialog.title)}</AlertDialogTitle>
            <AlertDialogDescription>{t(($) => $.voice_input.history.clear_dialog.description)}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.voice_input.history.clear_dialog.cancel)}</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={() => void api.clear()}>
              {t(($) => $.voice_input.history.clear)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsSection>
  );
}

function KeepHistoryRow({ api }: { api: LocalVoiceHistoryAPI }) {
  const { t } = useT("settings");
  const keepHistory = useVoiceSettingsStore((state) => state.keepHistory);
  const update = useVoiceSettingsStore((state) => state.update);
  const label = t(($) => $.voice_input.history.keep.title);
  const options = VOICE_HISTORY_RETENTIONS.map((value) => ({
    value,
    label: t(($) => $.voice_input.history.keep.options[value]),
  }));
  return (
    <SettingsRow
      anchor="voice-history-keep"
      label={label}
      description={t(($) => $.voice_input.history.keep.hint)}
      size="select"
    >
      <Select
        items={options}
        value={keepHistory}
        onValueChange={(next) => {
          if (!next) return;
          const retention = next as VoiceHistoryRetention;
          update({ keepHistory: retention });
          void api.setRetention(voiceHistoryDays(retention));
        }}
      >
        <SelectTrigger size="sm" className="w-full" aria-label={label}>
          <SelectValue>{options.find((option) => option.value === keepHistory)?.label}</SelectValue>
        </SelectTrigger>
        <SelectContent align="end">
          {options.map((option) => (
            <SelectItem key={option.value} value={option.value}>
              {option.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </SettingsRow>
  );
}

function HistoryList({ api, entries }: { api: LocalVoiceHistoryAPI; entries: VoiceHistoryEntry[] }) {
  const { t } = useT("settings");
  const locale = useLocale();
  const [shown, setShown] = useState(PAGE);
  const player = usePlayer(api);
  const modelId = useVoiceSettingsStore((state) => state.modelId);
  const canTranscribe = pickVoiceModel(useVoiceModels(), modelId) !== null;
  const groups = useMemo(() => {
    const today = dayKey(Date.now());
    const yesterday = dayKey(Date.now() - 24 * 60 * 60 * 1000);
    const dayLabel = new Intl.DateTimeFormat(locale, { month: "short", day: "numeric", weekday: "short" });
    const out: { key: string; label: string; entries: VoiceHistoryEntry[] }[] = [];
    for (const entry of entries.slice(0, shown)) {
      const key = dayKey(entry.createdAt);
      const last = out[out.length - 1];
      if (last?.key === key) {
        last.entries.push(entry);
        continue;
      }
      const label =
        key === today
          ? t(($) => $.voice_input.history.today)
          : key === yesterday
            ? t(($) => $.voice_input.history.yesterday)
            : dayLabel.format(entry.createdAt);
      out.push({ key, label, entries: [entry] });
    }
    return out;
  }, [entries, shown, locale, t]);
  const time = useMemo(() => new Intl.DateTimeFormat(locale, { hour: "numeric", minute: "2-digit" }), [locale]);

  const transcribeAgain = async (entry: VoiceHistoryEntry) => {
    const previous = entry.text;
    await api.update(entry.id, { status: "transcribing" });
    const kept = await api.audio(entry.id);
    const text = kept ? await transcribeVoiceClip(new Blob([kept.audio], { type: kept.mimeType })) : null;
    await api.update(entry.id, text ? { status: "done", text } : { status: previous ? "done" : "failed" });
    if (!text) toast.error(t(($) => $.voice_input.history.retry_failed));
  };
  const copy = async (text: string) => {
    if (await copyText(text)) toast.success(t(($) => $.voice_input.history.copied));
    else toast.error(t(($) => $.voice_input.history.copy_failed));
  };

  return (
    <div className="space-y-4">
      {groups.map((group) => (
        <div key={group.key} className="space-y-1.5">
          <h4 className="px-0.5 text-caption font-medium text-muted-foreground">{group.label}</h4>
          <SettingsCard>
            {group.entries.map((entry) => (
              <div
                key={entry.id}
                className="group/entry flex items-start gap-4 border-b border-surface-border px-4 py-3 last:border-b-0"
              >
                <div className="flex min-w-20 shrink-0 gap-2 whitespace-nowrap pt-0.5 text-caption tabular-nums text-muted-foreground">
                  <span>{time.format(entry.createdAt)}</span>
                  <span className="text-faint-foreground">{formatDuration(entry.durationMs)}</span>
                </div>
                <div className="min-w-0 flex-1 text-body">
                  {entry.status === "transcribing" ? (
                    <ShimmerText className="text-caption font-medium">
                      {t(($) => $.voice_input.history.status.transcribing)}
                    </ShimmerText>
                  ) : entry.text ? (
                    <p className="whitespace-pre-wrap break-words">{entry.text}</p>
                  ) : (
                    <p className={cn("text-caption", entry.status === "failed" ? "text-destructive" : "text-muted-foreground")}>
                      {entry.status === "failed"
                        ? t(($) => $.voice_input.history.status.failed)
                        : t(($) => $.voice_input.history.status.interrupted)}
                    </p>
                  )}
                </div>
                <div
                  className={cn(
                    "-my-1 flex shrink-0 items-center gap-0.5 transition-opacity",
                    // Recordings without text keep their actions in view: retrying is the point.
                    entry.text && player.playing !== entry.id &&
                      "opacity-0 group-hover/entry:opacity-100 group-focus-within/entry:opacity-100",
                  )}
                >
                  <IconButton
                    variant="ghost"
                    size="icon-sm"
                    label={
                      player.playing === entry.id
                        ? t(($) => $.voice_input.history.actions.pause)
                        : t(($) => $.voice_input.history.actions.play)
                    }
                    onClick={() => void player.toggle(entry.id)}
                  >
                    {player.playing === entry.id ? <Pause /> : <Play />}
                  </IconButton>
                  <IconButton
                    variant="ghost"
                    size="icon-sm"
                    label={t(($) => $.voice_input.history.actions.retranscribe)}
                    disabled={!canTranscribe || entry.status === "transcribing"}
                    onClick={() => void transcribeAgain(entry)}
                  >
                    <RotateCcw />
                  </IconButton>
                  {entry.text && (
                    <IconButton
                      variant="ghost"
                      size="icon-sm"
                      label={t(($) => $.voice_input.history.actions.copy)}
                      onClick={() => void copy(entry.text)}
                    >
                      <Copy />
                    </IconButton>
                  )}
                  <IconButton
                    variant="ghost"
                    size="icon-sm"
                    className="hover:text-destructive"
                    label={t(($) => $.voice_input.history.actions.delete)}
                    onClick={() => void api.remove(entry.id)}
                  >
                    <Trash2 />
                  </IconButton>
                </div>
              </div>
            ))}
          </SettingsCard>
        </div>
      ))}
      {entries.length > shown && (
        <div className="flex justify-center">
          <Button size="xs" variant="outline" onClick={() => setShown((n) => n + PAGE)}>
            {t(($) => $.voice_input.history.show_more)}
          </Button>
        </div>
      )}
    </div>
  );
}
