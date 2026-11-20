import type { ReactNode } from "react";
import type { Lang } from "./copy";
import { useStoryEnv } from "./env";

export interface ConceptInfo {
  letter: string;
  name: Record<Lang, string>;
  pitch: Record<Lang, string>;
  how: Record<Lang, string[]>;
  bestFor: Record<Lang, string>;
  tradeoff: Record<Lang, string>;
}

/** A concept's live demo with its name and how to drive it. */
export function DemoStage({ info, children }: { info: ConceptInfo; children: ReactNode }) {
  const { lang, source } = useStoryEnv();
  return (
    <div className="flex items-start gap-10 p-8">
      {children}
      <aside className="w-72 shrink-0 pt-2">
        <div className="text-caption font-medium text-brand">{info.letter}</div>
        <h1 className="mt-1 text-title font-semibold">{info.name[lang]}</h1>
        <p className="mt-2 text-body text-muted-foreground">{info.pitch[lang]}</p>
        <ul className="mt-5 space-y-2 text-body">
          {info.how[lang].map((step) => (
            <li key={step} className="flex gap-2">
              <span className="mt-2 size-1 shrink-0 rounded-full bg-faint-foreground" />
              <span>{step}</span>
            </li>
          ))}
        </ul>
        <dl className="mt-6 space-y-3 text-caption">
          <div>
            <dt className="font-medium text-foreground">{lang === "zh" ? "适合" : "Best for"}</dt>
            <dd className="mt-0.5 text-muted-foreground">{info.bestFor[lang]}</dd>
          </div>
          <div>
            <dt className="font-medium text-foreground">{lang === "zh" ? "代价" : "Trade-off"}</dt>
            <dd className="mt-0.5 text-muted-foreground">{info.tradeoff[lang]}</dd>
          </div>
        </dl>
        <p className="mt-6 text-caption text-faint-foreground">
          {source === "simulated"
            ? lang === "zh"
              ? "当前是模拟说话人。工具栏「Voice」切到「My microphone」可以用真麦克风试（Chrome 会做实时识别）。"
              : "Using the simulated speaker. Switch Voice → My microphone in the toolbar to try your own voice (Chrome transcribes live)."
            : lang === "zh"
              ? "正在使用你的麦克风。"
              : "Using your microphone."}
        </p>
      </aside>
    </div>
  );
}
