import { useEffect, useState, type ReactNode } from "react";
import {
  AlertCircle,
  CheckCircle2,
  Download,
  ExternalLink,
  Info,
  MicOff,
  ShieldCheck,
  Trash2,
  X,
} from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { Kbd } from "@multica/ui/components/ui/kbd";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@multica/ui/components/ui/select";
import { Switch } from "@multica/ui/components/ui/switch";
import { cn } from "@multica/ui/lib/utils";
import { useStoryEnv } from "../env";
import { CommentComposer } from "../composer/surfaces";
import { VoiceProvider } from "../composer/voice-provider";
import { ShortcutKeys } from "../composer/voice-ui";
import { SETTINGS_COPY, type SettingsCopy } from "./copy";
import { SegmentedToggle, SettingsCard, SettingsRow, SettingsSection, SettingsShell } from "./layout";
import { LOCAL_MODELS, formatSize, useVoiceSettings, type LocalModel, type ModelStatus } from "./store";

export function useSettingsCopy(): SettingsCopy {
  return SETTINGS_COPY[useStoryEnv().lang];
}

function SelectRow<T extends string>({
  label,
  description,
  value,
  options,
  onChange,
  size = "select",
}: {
  label: string;
  description?: string;
  value: T;
  options: Record<T, string>;
  onChange: (v: T) => void;
  size?: "select" | "select-wide";
}) {
  const items = (Object.keys(options) as T[]).map((k) => ({ value: k, label: options[k] }));
  return (
    <SettingsRow label={label} description={description} size={size}>
      <Select items={items} value={value} onValueChange={(v) => v && onChange(v as T)}>
        <SelectTrigger size="sm" className="w-full" aria-label={label}>
          <SelectValue>{options[value]}</SelectValue>
        </SelectTrigger>
        <SelectContent align="end">
          {items.map((o) => (
            <SelectItem key={o.value} value={o.value}>
              {o.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </SettingsRow>
  );
}

function SwitchRow({ label, description, checked, onChange }: { label: string; description?: string; checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <SettingsRow label={label} description={description}>
      <Switch checked={checked} onCheckedChange={onChange} aria-label={label} />
    </SettingsRow>
  );
}

/** A banner above the sections for anything that stops voice input working. */
function Notice({
  tone,
  icon,
  title,
  body,
  action,
}: {
  tone: "danger" | "info";
  icon: ReactNode;
  title: string;
  body: string;
  action?: ReactNode;
}) {
  return (
    <div
      role="note"
      className={cn(
        "flex items-start gap-3 rounded-xl border px-4 py-3",
        tone === "danger" ? "border-destructive/25 bg-destructive/5" : "border-surface-border bg-surface",
      )}
    >
      <span className={cn("mt-0.5 shrink-0 [&_svg]:size-4", tone === "danger" ? "text-destructive" : "text-muted-foreground")}>{icon}</span>
      <div className="min-w-0 flex-1">
        <p className="text-body font-medium">{title}</p>
        <p className="mt-0.5 text-caption leading-5 text-muted-foreground">{body}</p>
      </div>
      {action && <div className="shrink-0 self-center">{action}</div>}
    </div>
  );
}

export function VoiceNotices() {
  const copy = useSettingsCopy();
  const { settings, update, download, activeModel } = useVoiceSettings();
  const notices: ReactNode[] = [];
  if (settings.micPermission === "denied") {
    notices.push(
      <Notice
        key="denied"
        tone="danger"
        icon={<MicOff />}
        title={copy.deniedTitle}
        body={copy.deniedBody}
        action={
          <Button size="sm" variant="outline" onClick={() => update({ micPermission: "granted" })}>
            {copy.deniedAction}
            <ExternalLink data-icon="inline-end" />
          </Button>
        }
      />,
    );
  }
  if (settings.platform === "web") {
    notices.push(
      <Notice
        key="web"
        tone="info"
        icon={<Info />}
        title={copy.webTitle}
        body={copy.webBody}
        action={
          <Button size="sm" variant="outline">
            {copy.webAction}
          </Button>
        }
      />,
    );
  } else if (settings.engine === "local" && !activeModel) {
    const model = LOCAL_MODELS.find((m) => m.id === settings.model) ?? LOCAL_MODELS[0]!;
    const status = settings.models[model.id];
    if (status?.state !== "downloading") {
      notices.push(
        <Notice
          key="model"
          tone="danger"
          icon={<AlertCircle />}
          title={copy.noModelTitle}
          body={copy.noModelBody}
          action={
            <Button size="sm" variant="brand" onClick={() => download(model.id)}>
              <Download data-icon="inline-start" />
              {copy.noModelAction} {model.name} · {formatSize(model.sizeMb)}
            </Button>
          }
        />,
      );
    }
  }
  return notices.length ? <div className="space-y-3">{notices}</div> : null;
}

function ShortcutRecorder() {
  const copy = useSettingsCopy();
  const { settings, update } = useVoiceSettings();
  const [recording, setRecording] = useState(false);
  useEffect(() => {
    if (!recording) return;
    const down = (e: KeyboardEvent) => {
      e.preventDefault();
      e.stopPropagation();
      if (e.key === "Escape") return setRecording(false);
      if (["Alt", "Meta", "Control", "Shift"].includes(e.key)) return;
      const keys = [e.metaKey || e.ctrlKey ? "⌘" : "", e.altKey ? "⌥" : "", e.shiftKey ? "⇧" : ""].filter(Boolean);
      if (!keys.length) return;
      const main = e.code === "Space" ? "Space" : e.code.replace(/^Key/, "");
      update({ shortcut: [...keys, main] });
      setRecording(false);
    };
    window.addEventListener("keydown", down, true);
    return () => window.removeEventListener("keydown", down, true);
  }, [recording, update]);
  return (
    <button
      type="button"
      aria-label={copy.changeShortcut}
      onClick={() => setRecording(true)}
      className={cn(
        "flex h-8 min-w-28 items-center justify-center gap-2 rounded-md border px-2.5 text-caption transition-colors",
        recording ? "border-brand bg-brand/5 text-brand ring-3 ring-ring/30" : "border-input hover:bg-surface-hover",
      )}
    >
      {recording ? copy.pressKeys : <ShortcutKeys keys={settings.shortcut} />}
    </button>
  );
}

function Dots({ n, label }: { n: number; label: string }) {
  return (
    <span className="inline-flex items-center gap-1" title={label}>
      {label}
      <span className="inline-flex gap-0.5">
        {[1, 2, 3].map((i) => (
          <span key={i} className={cn("size-1.5 rounded-full", i <= n ? "bg-foreground/70" : "bg-foreground/15")} />
        ))}
      </span>
    </span>
  );
}

function ModelRow({ model, status, selected }: { model: LocalModel; status: ModelStatus; selected: boolean }) {
  const copy = useSettingsCopy();
  const { lang } = useStoryEnv();
  const { update, download, remove } = useVoiceSettings();
  const ready = status.state === "ready";
  return (
    <div className={cn("flex items-center gap-3 px-4 py-3", selected && ready && "bg-brand/[0.035]")}>
      <button
        type="button"
        aria-label={`${copy.use} ${model.name}`}
        disabled={!ready}
        onClick={() => update({ model: model.id })}
        className={cn(
          // eslint-disable-next-line no-restricted-syntax -- a radio
          "flex size-4 shrink-0 items-center justify-center rounded-full border transition-colors disabled:opacity-40",
          selected && ready ? "border-brand bg-brand" : "border-input",
        )}
      >
        {selected && ready && <span className="size-1.5 rounded-full bg-brand-foreground" />}
      </button>
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="text-body font-medium">{model.name}</span>
          {model.recommendedFor === lang && (
            <span className="rounded-full bg-brand/10 px-1.5 py-px text-micro font-medium text-brand">{copy.recommended[lang]}</span>
          )}
          {model.streaming && (
            <span className="rounded-full bg-muted px-1.5 py-px text-micro font-medium text-muted-foreground">{copy.streaming}</span>
          )}
        </div>
        <div className="mt-0.5 truncate text-caption text-muted-foreground">{model.languages[lang]}</div>
        <div className="mt-1 flex items-center gap-3 text-micro text-faint-foreground">
          <span className="tabular-nums">{formatSize(model.sizeMb)}</span>
          <Dots n={model.speed} label={copy.speed} />
          <Dots n={model.accuracy} label={copy.accuracy} />
        </div>
      </div>
      <div className="flex shrink-0 items-center justify-end gap-1">
        {status.state === "available" && (
          <Button size="xs" variant="outline" onClick={() => download(model.id)}>
            <Download data-icon="inline-start" />
            {copy.download}
          </Button>
        )}
        {status.state === "downloading" && (
          <div className="flex w-40 items-center gap-2">
            <div className="h-1.5 min-w-0 flex-1 overflow-hidden rounded-full bg-muted">
              <div className="h-full rounded-full bg-brand transition-[width] duration-100" style={{ width: `${status.progress}%` }} />
            </div>
            <span className="w-8 text-right text-micro tabular-nums text-muted-foreground">{Math.round(status.progress)}%</span>
            <IconButton label={copy.cancelDownload} onClick={() => remove(model.id)} className="text-muted-foreground">
              <X />
            </IconButton>
          </div>
        )}
        {ready &&
          (selected ? (
            <span className="flex items-center gap-1 px-2 text-caption font-medium text-brand">
              <CheckCircle2 className="size-3.5" />
              {copy.inUse}
            </span>
          ) : (
            <>
              <Button size="xs" variant="ghost" onClick={() => update({ model: model.id })}>
                {copy.use}
              </Button>
              <IconButton label={copy.deleteModel} onClick={() => remove(model.id)} className="text-muted-foreground">
                <Trash2 />
              </IconButton>
            </>
          ))}
      </div>
    </div>
  );
}

export function ShortcutCard() {
  const copy = useSettingsCopy();
  const { settings, update } = useVoiceSettings();
  return (
    <SettingsCard>
      <SettingsRow label={copy.shortcut} description={copy.shortcutHint}>
        <ShortcutRecorder />
      </SettingsRow>
      <SettingsRow label={copy.activation} description={copy.activationHint[settings.activation]} size="select-wide">
        <SegmentedToggle
          value={settings.activation}
          onChange={(activation) => update({ activation })}
          options={[
            ["auto", copy.activationOptions.auto],
            ["toggle", copy.activationOptions.toggle],
            ["hold", copy.activationOptions.hold],
          ]}
        />
      </SettingsRow>
      {settings.platform === "desktop" && (
        <SwitchRow
          label={copy.background}
          description={copy.backgroundHint}
          checked={settings.globalShortcut}
          onChange={(globalShortcut) => update({ globalShortcut })}
        />
      )}
      <SettingsRow label={copy.cancelKey} description={copy.cancelHint}>
        <Kbd>Esc</Kbd>
      </SettingsRow>
    </SettingsCard>
  );
}

export function EngineCard() {
  const copy = useSettingsCopy();
  const { settings, update } = useVoiceSettings();
  const web = settings.platform === "web";
  const engine = web ? "cloud" : settings.engine;
  return (
    <SettingsCard>
      <SettingsRow label={copy.engine} description={web ? copy.webBody : copy.engineHint[engine]} size="select">
        <SegmentedToggle
          value={engine}
          onChange={(e) => update({ engine: e })}
          disabled={web ? ["local"] : []}
          options={[
            ["local", copy.engineOptions.local],
            ["cloud", copy.engineOptions.cloud],
          ]}
        />
      </SettingsRow>
      {engine === "local" && (
        <div>
          <div className="px-4 pt-3 pb-1">
            <div className="text-body font-medium">{copy.models}</div>
            <div className="mt-0.5 text-caption text-muted-foreground">{copy.modelsHint}</div>
          </div>
          <div className="divide-y divide-surface-border">
            {LOCAL_MODELS.map((m) => (
              <ModelRow key={m.id} model={m} status={settings.models[m.id] ?? { state: "available" }} selected={settings.model === m.id} />
            ))}
          </div>
        </div>
      )}
      <SelectRow
        label={copy.language}
        value={settings.language}
        options={copy.languageOptions}
        onChange={(language) => update({ language })}
      />
      {(settings.language === "auto" || settings.language === "zh") && (
        <SettingsRow label={copy.chineseScript} size="select">
          <SegmentedToggle
            value={settings.chineseScript}
            onChange={(chineseScript) => update({ chineseScript })}
            options={[
              ["simplified", copy.scriptOptions.simplified],
              ["traditional", copy.scriptOptions.traditional],
            ]}
          />
        </SettingsRow>
      )}
    </SettingsCard>
  );
}

function useTestMeter(on: boolean) {
  const [level, setLevel] = useState(0);
  useEffect(() => {
    if (!on) return setLevel(0);
    const t0 = performance.now();
    const id = window.setInterval(() => {
      const t = performance.now() - t0;
      const speaking = Math.sin(t / 900) > -0.3;
      const v = speaking ? 0.25 + 0.55 * Math.abs(Math.sin(t / 80) * Math.sin(t / 210)) + Math.random() * 0.15 : 0.04 + Math.random() * 0.04;
      setLevel((l) => l * 0.4 + v * 0.6);
    }, 50);
    return () => window.clearInterval(id);
  }, [on]);
  return level;
}

function MicCard() {
  const copy = useSettingsCopy();
  const { settings, update } = useVoiceSettings();
  const [testing, setTesting] = useState(false);
  const level = useTestMeter(testing);
  const segments = 16;
  return (
    <SettingsCard>
      <SelectRow
        label={copy.device}
        value={settings.microphone as keyof SettingsCopy["devices"]}
        options={copy.devices}
        onChange={(microphone) => update({ microphone })}
        size="select-wide"
      />
      <SettingsRow label={copy.test} description={copy.testHint} size="select-wide">
        <div className="flex items-center gap-3">
          <div className="flex h-3 flex-1 items-center gap-[3px]" aria-hidden>
            {Array.from({ length: segments }, (_, i) => (
              <span
                key={i}
                className={cn(
                  "h-full flex-1 rounded-[1px] transition-colors duration-75",
                  i / segments < level ? (i > segments * 0.8 ? "bg-warning" : "bg-success") : "bg-muted",
                )}
              />
            ))}
          </div>
          <Button size="xs" variant="outline" onClick={() => setTesting((t) => !t)}>
            {testing ? copy.stopTest : copy.startTest}
          </Button>
        </div>
      </SettingsRow>
      <SwitchRow label={copy.sounds} description={copy.soundsHint} checked={settings.sounds} onChange={(sounds) => update({ sounds })} />
      {settings.platform === "desktop" && (
        <SwitchRow label={copy.muteOthers} checked={settings.muteOthers} onChange={(muteOthers) => update({ muteOthers })} />
      )}
    </SettingsCard>
  );
}

function Vocabulary() {
  const copy = useSettingsCopy();
  const { settings, update } = useVoiceSettings();
  const [draft, setDraft] = useState("");
  const add = () => {
    const w = draft.trim();
    if (w && !settings.vocabulary.includes(w)) update({ vocabulary: [...settings.vocabulary, w] });
    setDraft("");
  };
  return (
    <SettingsRow label={copy.vocabulary} description={copy.vocabularyHint} size="text" align="start">
      <div className="flex min-h-8 flex-wrap items-center gap-1 rounded-md border border-input px-1.5 py-1 focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/50">
        {settings.vocabulary.map((w) => (
          <span key={w} className="inline-flex h-6 items-center gap-1 rounded-sm bg-muted pl-2 pr-1 text-caption">
            {w}
            <button
              type="button"
              aria-label={`${copy.deleteModel} ${w}`}
              onClick={() => update({ vocabulary: settings.vocabulary.filter((x) => x !== w) })}
              className="flex size-4 items-center justify-center rounded-xs text-muted-foreground hover:bg-surface-hover hover:text-foreground"
            >
              <X className="size-3" />
            </button>
          </span>
        ))}
        <input
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.nativeEvent.isComposing) {
              e.preventDefault();
              add();
            }
          }}
          placeholder={copy.addWord}
          className="h-6 min-w-24 flex-1 bg-transparent px-1 text-caption outline-none placeholder:text-muted-foreground"
        />
      </div>
    </SettingsRow>
  );
}

function TextCard() {
  const copy = useSettingsCopy();
  const { settings, update } = useVoiceSettings();
  return (
    <SettingsCard>
      <SwitchRow
        label={copy.fillers}
        description={copy.fillersHint}
        checked={settings.removeFillers}
        onChange={(removeFillers) => update({ removeFillers })}
      />
      <SelectRow
        label={copy.polish}
        description={copy.polishHint}
        value={settings.polish}
        options={copy.polishOptions}
        onChange={(polish) => update({ polish })}
        size="select-wide"
      />
      <Vocabulary />
    </SettingsCard>
  );
}

function PrivacyCard() {
  const copy = useSettingsCopy();
  const { settings, update } = useVoiceSettings();
  const local = settings.platform === "desktop" && settings.engine === "local";
  return (
    <SettingsCard>
      <SelectRow
        label={copy.keep}
        description={copy.keepHint}
        value={settings.keepRecordings}
        options={copy.keepOptions}
        onChange={(keepRecordings) => update({ keepRecordings })}
      />
      <div className="flex items-center gap-2 px-4 py-3 text-caption text-muted-foreground">
        <ShieldCheck className="size-4 shrink-0" />
        {local ? copy.privacyLocal : copy.privacyCloud}
      </div>
    </SettingsCard>
  );
}

/** Settings → Voice input. Every control feeds the same store the composers read. */
export function VoiceSettingsPage() {
  const copy = useSettingsCopy();
  return (
    <SettingsShell copy={copy} title={copy.nav.voice} scope={copy.scopeDevice}>
      <div className="space-y-8">
        <p className="max-w-2xl text-body leading-6 text-muted-foreground">{copy.intro}</p>
        <VoiceNotices />
        <SettingsSection title={copy.shortcutSection}>
          <ShortcutCard />
        </SettingsSection>
        <SettingsSection title={copy.recognitionSection}>
          <EngineCard />
        </SettingsSection>
        <SettingsSection title={copy.micSection}>
          <MicCard />
        </SettingsSection>
        <SettingsSection title={copy.textSection}>
          <TextCard />
        </SettingsSection>
        <SettingsSection title={copy.privacySection}>
          <PrivacyCard />
        </SettingsSection>
        <SettingsSection title={copy.trySection} description={copy.trySectionHint}>
          <VoiceProvider primaryId="settings-try">
            <div className="pt-14">
              <CommentComposer id="settings-try" onSend={() => {}} trigger={false} />
            </div>
          </VoiceProvider>
        </SettingsSection>
      </div>
    </SettingsShell>
  );
}

