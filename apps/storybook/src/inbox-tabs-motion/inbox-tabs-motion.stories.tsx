import { useEffect, useRef, useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { Badge } from "@multica/ui/components/ui/badge";
import { cn } from "@multica/ui/lib/utils";
import { useStoryEnv } from "../voice-input/env";
import type { Lang } from "../voice-input/copy";
import type { TabId } from "./data";
import { InboxPanel, type MotionEnv, type PanelCommand } from "./inbox-panel";
import { OPTION_BY_ID, OPTIONS, type OptionId } from "./options";

const meta = {
  title: "Inbox tabs motion",
  id: "inbox-tabs-motion",
  parameters: { layout: "fullscreen" },
} satisfies Meta;
export default meta;
type Story = StoryObj<typeof meta>;

/** Each step waits, then selects a tab; selecting the open one folds the section. */
const SCRIPT: { wait: number; action: TabId; cold?: boolean }[] = [
  { wait: 700, action: "snoozed" },
  { wait: 1500, action: "archived", cold: true },
  { wait: 1500, action: "inbox" },
  { wait: 1500, action: "archived" },
  { wait: 1500, action: "archived" },
  { wait: 1300, action: "archived" },
  { wait: 1300, action: "inbox" },
];

function Segmented<T extends string | number>({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: T;
  options: { value: T; label: string }[];
  onChange: (value: T) => void;
}) {
  return (
    <div className="flex items-center gap-2">
      <span className="text-caption text-muted-foreground">{label}</span>
      <div className="flex rounded-lg bg-surface p-0.5 ring-1 ring-surface-border">
        {options.map((option) => (
          <button
            key={String(option.value)}
            type="button"
            aria-pressed={option.value === value}
            onClick={() => onChange(option.value)}
            className={cn(
              "h-6 rounded-md px-2 text-caption font-medium outline-none focus-visible:outline-2 focus-visible:outline-ring",
              option.value === value ? "bg-surface-selected text-foreground" : "text-muted-foreground hover:text-foreground",
            )}
          >
            {option.label}
          </button>
        ))}
      </div>
    </div>
  );
}

function useAutoplay(env: MotionEnv) {
  const [command, setCommand] = useState<PanelCommand>({ seq: 0, action: "reset" });
  const [playing, setPlaying] = useState(false);
  const timers = useRef<number[]>([]);
  const send = (action: PanelCommand["action"]) => setCommand((c) => ({ seq: c.seq + 1, action }));
  const stop = () => {
    timers.current.forEach((t) => window.clearTimeout(t));
    timers.current = [];
    setPlaying(false);
  };
  const play = () => {
    stop();
    send("reset");
    setPlaying(true);
    let at = 0;
    let latency = 0;
    for (const step of SCRIPT) {
      at += step.wait / env.speed + latency;
      latency = step.cold ? env.latency : 0;
      timers.current.push(window.setTimeout(() => send(step.action), at));
    }
    timers.current.push(window.setTimeout(() => setPlaying(false), at + 600 / env.speed));
  };
  useEffect(() => stop, []);
  return { command, playing, play, reset: () => (stop(), send("reset")) };
}

function Controls({
  env,
  setEnv,
  lang,
  playing,
  onPlay,
  onReset,
}: {
  env: MotionEnv;
  setEnv: (env: MotionEnv) => void;
  lang: Lang;
  playing: boolean;
  onPlay: () => void;
  onReset: () => void;
}) {
  const zh = lang === "zh";
  return (
    <div className="flex flex-wrap items-center gap-x-5 gap-y-2">
      <button
        type="button"
        onClick={onPlay}
        className="h-7 rounded-md bg-brand px-3 text-caption font-medium text-brand-foreground outline-none hover:opacity-90 focus-visible:outline-2 focus-visible:outline-ring"
      >
        {playing ? (zh ? "重新播放" : "Replay") : zh ? "▶ 自动演示" : "▶ Play demo"}
      </button>
      <button
        type="button"
        onClick={onReset}
        className="h-7 rounded-md px-2 text-caption font-medium text-muted-foreground outline-none hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring"
      >
        {zh ? "重置（清空已归档缓存）" : "Reset (cold Archived)"}
      </button>
      <Segmented
        label={zh ? "已归档首次加载" : "Archived first load"}
        value={env.latency}
        options={[0, 250, 600].map((ms) => ({ value: ms, label: `${ms}ms` }))}
        onChange={(latency) => setEnv({ ...env, latency })}
      />
      <Segmented
        label={zh ? "速度" : "Speed"}
        value={env.speed}
        options={[
          { value: 1, label: "1×" },
          { value: 0.25, label: zh ? "0.25× 慢放" : "0.25× slow" },
        ]}
        onChange={(speed) => setEnv({ ...env, speed })}
      />
      <Segmented
        label={zh ? "减少动态效果" : "Reduce motion"}
        value={env.reduce ? "on" : "off"}
        options={[
          { value: "off", label: zh ? "关" : "Off" },
          { value: "on", label: zh ? "开" : "On" },
        ]}
        onChange={(value) => setEnv({ ...env, reduce: value === "on" })}
      />
    </div>
  );
}

function OptionCard({ id, lang }: { id: OptionId; lang: Lang }) {
  const option = OPTION_BY_ID[id];
  return (
    <article className="flex flex-col rounded-xl bg-surface p-4 ring-1 ring-surface-border">
      <div className="flex items-center justify-between gap-2">
        <span className="text-caption font-semibold text-brand">{option.letter}</span>
        {option.recommended && (
          <Badge variant="secondary" className="text-micro font-medium text-brand">
            {lang === "zh" ? "推荐" : "Recommended"}
          </Badge>
        )}
      </div>
      <h2 className="mt-1 text-body font-semibold text-foreground">{option.name[lang]}</h2>
      <p className="mt-1.5 text-caption text-muted-foreground">{option.pitch[lang]}</p>
      <ul className="mt-3 space-y-1.5 text-caption text-foreground/85">
        {option.mechanics[lang].map((line) => (
          <li key={line} className="flex gap-1.5">
            <span className="mt-1.5 size-1 shrink-0 rounded-full bg-faint-foreground" />
            <span>{line}</span>
          </li>
        ))}
      </ul>
    </article>
  );
}

const DEFAULT_ENV: MotionEnv = { latency: 600, speed: 1, reduce: false };

function ComparePage() {
  const { lang } = useStoryEnv();
  const [env, setEnv] = useState(DEFAULT_ENV);
  const { command, playing, play, reset } = useAutoplay(env);
  const zh = lang === "zh";
  return (
    <div className="min-h-screen bg-page-canvas">
      <div className="mx-auto max-w-[1320px] px-8 py-8">
        <header className="max-w-3xl">
          <h1 className="text-display-sm font-semibold">
            {zh ? "Inbox 区块头：标签切换动效" : "Inbox section header: tab switch motion"}
          </h1>
          <p className="mt-2 text-body text-muted-foreground">
            {zh
              ? "四个面板都是 280px 默认宽度，标签样式和 #11268 上线的一致。点「自动演示」四个面板会同步切换：已延后 → 已归档（首次加载，带延迟）→ 收件箱 → 已归档（已缓存）→ 点当前标签折叠 → 再展开 → 收件箱。也可以直接点任意面板里的标签。切到 0.25× 慢放能看清每一帧。"
              : "Each panel is the 280px default width with the tabs as shipped in #11268. Play the demo to switch all four in step: Snoozed → Archived (first load, with latency) → Inbox → Archived (cached) → fold by selecting the open tab → unfold → Inbox. Or click tabs in any panel. Slow it to 0.25× to see each frame."}
          </p>
        </header>
        <div className="mt-5">
          <Controls env={env} setEnv={setEnv} lang={lang} playing={playing} onPlay={play} onReset={reset} />
        </div>
        <div className="mt-6 grid grid-cols-4 gap-5">
          {OPTIONS.map((option) => (
            <div key={option.id} data-option={option.id} className="flex flex-col gap-2">
              <div className="flex items-baseline gap-1.5 text-caption">
                <span className="font-semibold text-brand">{option.letter}</span>
                <span className="font-medium text-foreground">{option.name[lang]}</span>
              </div>
              <InboxPanel option={option} env={env} lang={lang} command={command} />
            </div>
          ))}
        </div>
        <section className="mt-8 grid grid-cols-4 gap-5">
          {OPTIONS.map((option) => (
            <OptionCard key={option.id} id={option.id} lang={lang} />
          ))}
        </section>
      </div>
    </div>
  );
}

function TryPage({ id }: { id: OptionId }) {
  const { lang } = useStoryEnv();
  const [env, setEnv] = useState(DEFAULT_ENV);
  const { command, playing, play, reset } = useAutoplay(env);
  return (
    <div className="min-h-screen bg-page-canvas">
      <div className="mx-auto flex max-w-[960px] gap-8 px-8 py-8">
        <div data-option={id}>
          <InboxPanel option={OPTION_BY_ID[id]} env={env} lang={lang} command={command} />
        </div>
        <div className="flex min-w-0 flex-1 flex-col gap-5">
          <Controls env={env} setEnv={setEnv} lang={lang} playing={playing} onPlay={play} onReset={reset} />
          <OptionCard id={id} lang={lang} />
        </div>
      </div>
    </div>
  );
}

export const CompareAll: Story = { name: "Compare all", render: () => <ComparePage /> };
export const TryA: Story = { name: "A · Sliding highlight (try it)", render: () => <TryPage id="slide" /> };
export const TryB: Story = { name: "B · Unfold in place (try it)", render: () => <TryPage id="inplace" /> };
export const TryC: Story = { name: "C · A + directional list (try it)", render: () => <TryPage id="direction" /> };
export const Today: Story = { name: "Today · No motion (try it)", render: () => <TryPage id="none" /> };
