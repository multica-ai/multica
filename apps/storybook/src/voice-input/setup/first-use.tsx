import { useEffect, useRef, useState } from "react";
import { Check, CheckCircle2, Cloud, Download, Laptop, Mic, X } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { cn } from "@multica/ui/lib/utils";
import type { Lang, VoiceScript } from "../copy";
import { useStoryEnv } from "../env";
import { RecDot, Shimmer, Waveform } from "../primitives";
import { ShortcutKeys } from "../composer/voice-ui";
import { LOCAL_MODELS, formatSize, useVoiceSettings, type Engine } from "../settings/store";
import { useVoiceSession, type VoiceSession } from "../voice-session";

export type SetupStep = "permission" | "engine" | "try" | "done";

const STEPS: SetupStep[] = ["permission", "engine", "try", "done"];

const TRY_SCRIPT: Record<Lang, VoiceScript> = {
  en: { phrases: ["Can we move tomorrow's design review", " to three?"], polished: "Can we move tomorrow's design review to three?" },
  zh: { phrases: ["明天的设计评审", "能改到三点吗？"], polished: "明天的设计评审能改到三点吗？" },
};

const C = {
  en: {
    stepNames: { permission: "Microphone", engine: "Engine", try: "Try it", done: "Done" },
    permissionTitle: "Talk instead of typing",
    permissionBody: "Dictate into chat, comments and replies. Two quick steps first.",
    permissionPoints: ["Tap the mic to dictate, or hold it to talk.", "Audio is only used to turn what you say into text."],
    allow: "Allow microphone",
    waiting: "Waiting for macOS…",
    engineTitle: "Where should speech be recognized?",
    local: "On this device",
    localBody: "Works offline and audio never leaves this computer.",
    localNeeds: "Downloads",
    cloud: "Multica Cloud",
    cloudBody: "Ready right away. Audio is sent to Multica and deleted after.",
    recommended: "Recommended",
    continue: "Continue",
    downloading: "Downloading",
    useCloudMeanwhile: "Use the cloud while it downloads",
    tryTitle: "Try it",
    tryBody: "Hold the button, or hold the shortcut, and read this out:",
    holdHere: "Hold to talk",
    heard: "Got it",
    again: "Try again",
    doneTitle: "You're set",
    doneShortcut: "Shortcut",
    doneEngine: "Engine",
    doneHint: "Change these anytime in Settings → Voice input.",
    finish: "Start dictating",
    skip: "Skip",
  },
  zh: {
    stepNames: { permission: "麦克风", engine: "识别方式", try: "试一下", done: "完成" },
    permissionTitle: "用说的，不用打字",
    permissionBody: "在聊天、评论和回复里都能用语音输入。先做两步设置。",
    permissionPoints: ["点一下麦克风开始听写，按住就是边说边录。", "音频只用来把你说的话转成文字。"],
    allow: "允许使用麦克风",
    waiting: "等待 macOS 授权…",
    engineTitle: "在哪里识别语音？",
    local: "本机",
    localBody: "离线可用，音频不会离开这台电脑。",
    localNeeds: "需要下载",
    cloud: "Multica 云端",
    cloudBody: "马上能用。音频发送到 Multica 识别，识别完即删除。",
    recommended: "推荐",
    continue: "继续",
    downloading: "正在下载",
    useCloudMeanwhile: "下载期间先用云端",
    tryTitle: "试一下",
    tryBody: "按住下面的按钮或快捷键，读出这句话：",
    holdHere: "按住说话",
    heard: "识别成功",
    again: "再试一次",
    doneTitle: "设置好了",
    doneShortcut: "快捷键",
    doneEngine: "识别方式",
    doneHint: "随时可以在「设置 → 语音输入」里修改。",
    finish: "开始使用",
    skip: "跳过",
  },
};

function StepDots({ step, lang }: { step: SetupStep; lang: Lang }) {
  const at = STEPS.indexOf(step);
  return (
    <ol className="flex items-center gap-2 text-micro">
      {STEPS.slice(0, 3).map((s, i) => (
        <li key={s} className="flex items-center gap-2">
          <span
            className={cn(
              "flex size-4 items-center justify-center rounded-full text-[10px] font-semibold",
              i < at ? "bg-brand text-brand-foreground" : i === at ? "bg-foreground text-background" : "bg-muted text-muted-foreground",
            )}
          >
            {i < at ? <Check className="size-2.5" /> : i + 1}
          </span>
          <span className={cn(i === at ? "font-medium text-foreground" : "text-muted-foreground")}>{C[lang].stepNames[s]}</span>
          {i < 2 && <span className="h-px w-5 bg-border" />}
        </li>
      ))}
    </ol>
  );
}

function EngineOption({
  selected,
  onSelect,
  icon,
  title,
  body,
  badge,
  extra,
}: {
  selected: boolean;
  onSelect: () => void;
  icon: React.ReactNode;
  title: string;
  body: string;
  badge?: string;
  extra?: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onSelect}
      aria-pressed={selected}
      className={cn(
        "flex w-full items-start gap-3 rounded-lg border p-3.5 text-left transition-colors",
        selected ? "border-brand bg-brand/5 ring-1 ring-brand" : "border-input hover:bg-surface-hover",
      )}
    >
      <span className={cn("mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-md", selected ? "bg-brand/12 text-brand" : "bg-muted text-muted-foreground")}>
        {icon}
      </span>
      <span className="min-w-0 flex-1">
        <span className="flex items-center gap-1.5">
          <span className="text-body font-medium">{title}</span>
          {badge && <span className="rounded-full bg-brand/10 px-1.5 py-px text-micro font-medium text-brand">{badge}</span>}
        </span>
        <span className="mt-0.5 block text-caption text-muted-foreground">{body}</span>
        {extra}
      </span>
    </button>
  );
}

function TryPad({ session, lang, onPress, onRelease }: { session: VoiceSession; lang: Lang; onPress: () => void; onRelease: () => void }) {
  const c = C[lang];
  const { phase } = session;
  const listening = phase === "listening" || phase === "requesting";
  return (
    <div className="flex flex-col items-center gap-4 rounded-lg bg-surface px-4 py-6 ring-1 ring-surface-border">
      <p className="text-center text-title-sm font-medium">“{TRY_SCRIPT[lang].polished}”</p>
      <div className="flex h-6 items-center">
        {listening ? (
          <span className="flex items-center gap-2">
            <RecDot />
            <Waveform levels={session.levels} bars={40} className="h-6 w-40 text-foreground" />
          </span>
        ) : phase === "processing" ? (
          <Shimmer>{lang === "zh" ? "正在转写…" : "Transcribing…"}</Shimmer>
        ) : null}
      </div>
      <button
        type="button"
        onPointerDown={(e) => {
          e.currentTarget.setPointerCapture(e.pointerId);
          onPress();
        }}
        onPointerUp={onRelease}
        aria-label={c.holdHere}
        className={cn(
          // eslint-disable-next-line no-restricted-syntax -- the composer send button's hold-to-talk twin, the setup's one primary control
          "flex size-16 touch-none items-center justify-center rounded-full transition-all",
          listening ? "scale-110 bg-brand text-brand-foreground shadow-[0_0_0_8px_color-mix(in_oklab,var(--brand)_18%,transparent)]" : "bg-foreground text-background hover:scale-105",
        )}
      >
        <Mic className="size-6" />
      </button>
      <span className="text-caption text-muted-foreground">{c.holdHere}</span>
    </div>
  );
}

/**
 * First-use setup, shown the first time someone presses the mic. Modeled on
 * Handy's onboarding (permissions → model → try it), trimmed to what Multica
 * needs before the first dictation.
 */
export function FirstUseDialog({
  initialStep = "permission",
  frozen,
  onClose,
}: {
  initialStep?: SetupStep;
  /** Pin the try step's session, for the gallery. */
  frozen?: VoiceSession;
  onClose?: () => void;
}) {
  const { lang, source } = useStoryEnv();
  const c = C[lang];
  const { settings, update, download } = useVoiceSettings();
  const [step, setStep] = useState<SetupStep>(initialStep);
  const [asking, setAsking] = useState(false);
  const [choice, setChoice] = useState<Engine>(settings.engine);
  const model = LOCAL_MODELS.find((m) => m.recommendedFor === lang) ?? LOCAL_MODELS[0]!;
  const status = settings.models[model.id];
  const [heard, setHeard] = useState("");

  const live = useVoiceSession({ source, lang, script: TRY_SCRIPT[lang], processingMs: 600 });
  const session = frozen ?? live;
  const liveRef = useRef(live);
  liveRef.current = live;
  useEffect(() => {
    if (liveRef.current.phase === "done") {
      setHeard(liveRef.current.result);
      liveRef.current.reset();
    }
  }, [live.phase]);

  useEffect(() => {
    if (step !== "try" || frozen) return;
    const down = (e: KeyboardEvent) => {
      if (e.code === "Space" && e.altKey) {
        e.preventDefault();
        if (!e.repeat && liveRef.current.phase === "idle") liveRef.current.start();
      }
    };
    const up = (e: KeyboardEvent) => {
      if ((e.code === "Space" || e.key === "Alt") && liveRef.current.phase === "listening") liveRef.current.stop();
    };
    window.addEventListener("keydown", down);
    window.addEventListener("keyup", up);
    return () => {
      window.removeEventListener("keydown", down);
      window.removeEventListener("keyup", up);
    };
  }, [step, frozen]);

  const allow = () => {
    setAsking(true);
    window.setTimeout(() => {
      setAsking(false);
      update({ micPermission: "granted" });
      setStep("engine");
    }, 1100);
  };

  const continueFromEngine = () => {
    update({ engine: choice, model: model.id });
    if (choice === "local" && status?.state !== "ready") download(model.id);
    setStep("try");
  };

  const downloading = settings.models[model.id]?.state === "downloading";
  const progress = (() => {
    const s = settings.models[model.id];
    return s?.state === "downloading" ? s.progress : s?.state === "ready" ? 100 : 0;
  })();

  return (
    <div role="dialog" aria-label={c.permissionTitle} className="w-[480px] overflow-hidden rounded-xl bg-surface-raised shadow-[var(--floating-shadow)] ring-1 ring-floating-border">
      <div className="flex items-center justify-between border-b px-5 py-3">
        <StepDots step={step} lang={lang} />
        <IconButton label={c.skip} onClick={onClose} className="text-muted-foreground">
          <X />
        </IconButton>
      </div>
      <div className="px-6 py-6">
        {step === "permission" && (
          <>
            <span className="flex size-10 items-center justify-center rounded-lg bg-brand/10 text-brand">
              <Mic className="size-5" />
            </span>
            <h2 className="mt-4 text-title font-semibold">{c.permissionTitle}</h2>
            <p className="mt-1.5 text-body text-muted-foreground">{c.permissionBody}</p>
            <ul className="mt-4 space-y-2 text-body">
              {c.permissionPoints.map((p) => (
                <li key={p} className="flex gap-2">
                  <Check className="mt-1 size-3.5 shrink-0 text-brand" />
                  {p}
                </li>
              ))}
            </ul>
            <Button className="mt-6 w-full" variant="brand" onClick={allow} disabled={asking}>
              {asking ? <Shimmer className="[--foreground:currentColor]">{c.waiting}</Shimmer> : c.allow}
            </Button>
          </>
        )}

        {step === "engine" && (
          <>
            <h2 className="text-title font-semibold">{c.engineTitle}</h2>
            <div className="mt-4 space-y-2.5">
              <EngineOption
                selected={choice === "local"}
                onSelect={() => setChoice("local")}
                icon={<Laptop className="size-4" />}
                title={c.local}
                badge={c.recommended}
                body={c.localBody}
                extra={
                  <span className="mt-2 flex items-center gap-1.5 text-micro text-faint-foreground">
                    <Download className="size-3" />
                    {c.localNeeds} {model.name} · {formatSize(model.sizeMb)}
                  </span>
                }
              />
              <EngineOption
                selected={choice === "cloud"}
                onSelect={() => setChoice("cloud")}
                icon={<Cloud className="size-4" />}
                title={c.cloud}
                body={c.cloudBody}
              />
            </div>
            <Button className="mt-6 w-full" variant="brand" onClick={continueFromEngine}>
              {c.continue}
            </Button>
          </>
        )}

        {step === "try" && (
          <>
            <h2 className="text-title font-semibold">{c.tryTitle}</h2>
            <p className="mt-1.5 text-body text-muted-foreground">{c.tryBody}</p>
            {settings.engine === "local" && progress < 100 && (
              <div className="mt-4 rounded-lg border border-surface-border px-3 py-2.5">
                <div className="flex items-center justify-between text-caption">
                  <span>
                    {c.downloading} {model.name}
                  </span>
                  <span className="tabular-nums text-muted-foreground">{Math.round(progress)}%</span>
                </div>
                <div className="mt-2 h-1.5 overflow-hidden rounded-full bg-muted">
                  <div className="h-full rounded-full bg-brand transition-[width]" style={{ width: `${progress}%` }} />
                </div>
                {downloading && (
                  <button type="button" onClick={() => update({ engine: "cloud" })} className="mt-2 text-caption text-brand hover:underline">
                    {c.useCloudMeanwhile}
                  </button>
                )}
              </div>
            )}
            <div className="mt-4">
              {heard && !frozen ? (
                <div className="flex flex-col items-center gap-3 rounded-lg bg-success/8 px-4 py-6 ring-1 ring-success/25">
                  <CheckCircle2 className="size-6 text-success" />
                  <p className="text-center text-title-sm font-medium">{heard}</p>
                  <span className="text-caption text-muted-foreground">{c.heard}</span>
                </div>
              ) : (
                <TryPad session={session} lang={lang} onPress={() => live.start()} onRelease={() => live.stop()} />
              )}
            </div>
            <div className="mt-6 flex gap-2">
              {heard && (
                <Button variant="outline" className="flex-1" onClick={() => setHeard("")}>
                  {c.again}
                </Button>
              )}
              <Button variant="brand" className="flex-1" disabled={!heard && !frozen} onClick={() => setStep("done")}>
                {c.continue}
              </Button>
            </div>
          </>
        )}

        {step === "done" && (
          <>
            <span className="flex size-10 items-center justify-center rounded-lg bg-success/10 text-success">
              <CheckCircle2 className="size-5" />
            </span>
            <h2 className="mt-4 text-title font-semibold">{c.doneTitle}</h2>
            <dl className="mt-4 divide-y divide-surface-border rounded-lg border border-surface-border text-body">
              <div className="flex items-center justify-between px-3.5 py-2.5">
                <dt className="text-muted-foreground">{c.doneShortcut}</dt>
                <dd>
                  <ShortcutKeys keys={settings.shortcut} />
                </dd>
              </div>
              <div className="flex items-center justify-between px-3.5 py-2.5">
                <dt className="text-muted-foreground">{c.doneEngine}</dt>
                <dd>{settings.engine === "local" ? `${c.local} · ${model.name}` : c.cloud}</dd>
              </div>
            </dl>
            <p className="mt-3 text-caption text-muted-foreground">{c.doneHint}</p>
            <Button className="mt-6 w-full" variant="brand" onClick={onClose}>
              {c.finish}
            </Button>
          </>
        )}
      </div>
    </div>
  );
}
