import type { ReactNode } from "react";
import { CHAT_SCRIPT, type Lang, type VoiceScript } from "./copy";
import { mockSession, type VoiceSession } from "./voice-session";

/** Frozen sessions at the interesting points of one dictation. */
export function samples(lang: Lang, script: VoiceScript = CHAT_SCRIPT[lang]) {
  const [p0 = "", p1 = "", p2 = ""] = script.phrases;
  const cut = lang === "zh" ? p2.indexOf(" ") : p2.indexOf(" ", 18);
  return {
    idle: mockSession(),
    requesting: mockSession({ phase: "requesting" }),
    listeningEmpty: mockSession({ phase: "listening", elapsedMs: 900, speaking: false }),
    listening: mockSession({
      phase: "listening",
      elapsedMs: 7400,
      finalText: p0 + p1,
      interimText: p2.slice(0, cut),
    }),
    paused: mockSession({ phase: "paused", elapsedMs: 9100, finalText: p0 + p1 + p2, speaking: false }),
    processing: mockSession({ phase: "processing", elapsedMs: 12300, finalText: script.phrases.join("") }),
    denied: mockSession({ phase: "error", error: "permission-denied" }),
    noSpeech: mockSession({ phase: "error", error: "no-speech" }),
    network: mockSession({ phase: "error", error: "network" }),
  } satisfies Record<string, VoiceSession>;
}

export function Gallery({ items, columns = 3 }: { items: { label: string; node: ReactNode }[]; columns?: number }) {
  return (
    <div className="grid gap-x-8 gap-y-10 p-8" style={{ gridTemplateColumns: `repeat(${columns}, max-content)` }}>
      {items.map((item) => (
        <figure key={item.label} className="flex flex-col gap-3">
          <figcaption className="text-caption font-medium text-muted-foreground">{item.label}</figcaption>
          {item.node}
        </figure>
      ))}
    </div>
  );
}
