import { useEffect, useRef, useState, type ReactNode } from "react";
import { Bot, ChevronRight, Circle, FolderKanban, Maximize2, Mic, SignalHigh, Sparkles, Square, X } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { cn } from "@multica/ui/lib/utils";
import { ISSUE_FIELDS, ISSUE_SCRIPT } from "../copy";
import { useStoryEnv } from "../env";
import { Elapsed, InlineVoiceError, LiveTranscript, RecDot, Shimmer, Waveform } from "../primitives";
import { useWindowKeys } from "../use-dictation";
import { useVoiceSession, type VoiceSession } from "../voice-session";
import { TOGGLE_KEYS } from "./inline-waveform";

/** Same chrome as packages/views/common/pill-button.tsx. */
function Pill({ icon, children, fromVoice }: { icon: ReactNode; children: ReactNode; fromVoice?: boolean }) {
  return (
    <button
      type="button"
      className={cn(
        "inline-flex h-[var(--button-height-xs)] min-w-0 max-w-56 cursor-pointer items-center gap-1.5 overflow-hidden rounded-md border px-2 text-caption transition-colors hover:bg-accent/60",
        fromVoice && "border-brand/40 bg-brand/7 animate-in fade-in zoom-in-95 duration-300",
      )}
    >
      {icon}
      <span className="truncate">{children}</span>
    </button>
  );
}

/**
 * F. Voice to issue — say the whole thing; Multica fills in the title, the
 * description and the properties it heard, and marks what came from your
 * voice so you can check it before creating.
 */
export function VoiceToIssue({ frozen, frozenStep }: { frozen?: VoiceSession; frozenStep?: number }) {
  const { lang, source, copy } = useStoryEnv();
  const script = ISSUE_SCRIPT[lang];
  const fields = ISSUE_FIELDS[lang];
  const live = useVoiceSession({ source, lang, script, polish: true, processingMs: 1500 });
  const session = frozen ?? live;
  // 0 empty · 1 title · 2 description · 3 properties
  const [liveStep, setStep] = useState(0);
  const step = frozenStep ?? liveStep;
  const [description, setDescription] = useState("");
  const timersRef = useRef<number[]>([]);
  const { phase } = session;

  useEffect(() => () => timersRef.current.forEach(window.clearTimeout), []);

  useEffect(() => {
    if (frozen || phase !== "done") return;
    setDescription(session.result.replace(/^(new issue:\s*|新建一个 issue：)/i, ""));
    // Properties are only filled once the speaker has actually said them.
    const heardProperties = session.finalText.trim() === script.phrases.join("").trim();
    const steps = heardProperties ? [1, 2, 3] : [1, 2];
    timersRef.current = steps.map((s) => window.setTimeout(() => setStep(s), s * 280));
    session.reset();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [phase]);

  const start = () => {
    timersRef.current.forEach(window.clearTimeout);
    setStep(0);
    setDescription("");
    session.start();
  };

  useWindowKeys(
    {
      down: (e) => {
        if (e.code === "Space" && e.shiftKey && (e.metaKey || e.ctrlKey)) {
          e.preventDefault();
          if (phase === "listening") session.stop();
          else if (phase === "idle" || phase === "error") start();
        } else if (e.key === "Escape" && phase === "listening") {
          session.cancel();
        }
      },
    },
    !frozen,
  );

  const listening = phase === "listening" || phase === "requesting";
  const shownDescription = frozen && step >= 2 ? script.polished : description;

  return (
    <div className="flex h-[440px] w-[640px] flex-col overflow-hidden rounded-xl bg-popover shadow-[var(--floating-shadow)] ring-1 ring-floating-border">
      <div className="flex items-center justify-between px-5 pb-2 pt-3">
        <div className="flex items-center gap-1.5 text-caption">
          <span className="text-muted-foreground">Multica</span>
          <ChevronRight className="size-3 text-faint-foreground" />
          <span className="font-medium">{copy.newIssue}</span>
        </div>
        <div className="flex items-center gap-1">
          <IconButton label="Expand" className="text-muted-foreground">
            <Maximize2 />
          </IconButton>
          <IconButton label={copy.cancel} className="text-muted-foreground">
            <X />
          </IconButton>
        </div>
      </div>

      <div className="px-5 pb-2">
        {phase === "processing" ? (
          <Shimmer className="text-title font-semibold">{copy.understood}…</Shimmer>
        ) : step >= 1 ? (
          <div className="flex items-center gap-2 text-title font-semibold animate-in fade-in slide-in-from-bottom-1">
            {fields.title}
          </div>
        ) : (
          <div className="text-title font-semibold text-faint-foreground">{copy.issueTitle}</div>
        )}
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto px-5 text-body leading-relaxed">
        {listening || phase === "processing" ? (
          <LiveTranscript
            finalText={session.finalText}
            interimText={session.interimText}
            caret={phase === "listening"}
            className={cn(phase === "processing" && "text-muted-foreground")}
          />
        ) : step >= 2 ? (
          <p className="animate-in fade-in">{shownDescription}</p>
        ) : (
          <span className="text-faint-foreground">{copy.issueDescription}…</span>
        )}
        {phase === "error" && session.error && (
          <div className="mt-2">
            <InlineVoiceError error={session.error} copy={copy} onRetry={start} onDismiss={session.cancel} />
          </div>
        )}
      </div>

      <div className="flex flex-wrap items-center gap-1.5 px-4 py-2">
        <Pill icon={<Circle className="size-3.5 text-muted-foreground" />}>{copy.status}</Pill>
        <Pill
          icon={<SignalHigh className={cn("size-3.5", step >= 3 ? "text-priority-high" : "text-muted-foreground")} />}
          fromVoice={step >= 3}
        >
          {step >= 3 ? fields.priority : copy.priority}
        </Pill>
        <Pill
          icon={
            step >= 3 ? (
              <span className="flex size-4 items-center justify-center rounded-full bg-brand/12 text-brand">
                <Bot className="size-2.5" />
              </span>
            ) : (
              <Circle className="size-3.5 text-muted-foreground" />
            )
          }
          fromVoice={step >= 3}
        >
          {step >= 3 ? fields.assignee : copy.assignee}
        </Pill>
        <Pill icon={<FolderKanban className="size-3.5 text-muted-foreground" />} fromVoice={step >= 3}>
          {step >= 3 ? fields.project : copy.project}
        </Pill>
      </div>

      <div className="flex items-center gap-2 border-t px-4 py-3">
        {listening ? (
          <div className="flex min-w-0 flex-1 items-center gap-2">
            <RecDot />
            <Elapsed ms={session.elapsedMs} />
            <Waveform levels={session.levels} bars={40} className="w-40 shrink-0 overflow-hidden text-foreground" />
            <Button size="sm" variant="outline" onClick={session.stop} disabled={phase !== "listening"}>
              <Square className="size-3 fill-current" />
              {copy.done}
            </Button>
          </div>
        ) : step >= 3 ? (
          <span className="flex flex-1 items-center gap-1.5 text-caption text-muted-foreground">
            <Sparkles className="size-3.5 text-brand" />
            {copy.understood}
          </span>
        ) : (
          <div className="flex-1">
            <IconButton
              label={copy.dictate}
              shortcut={TOGGLE_KEYS}
              variant="outline"
              onClick={start}
              disabled={phase === "processing"}
              className="text-muted-foreground"
            >
              <Mic />
            </IconButton>
          </div>
        )}
        <Button size="sm" disabled={step < 1}>
          {copy.createIssue}
        </Button>
      </div>
    </div>
  );
}
