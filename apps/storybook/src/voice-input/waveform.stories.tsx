import { useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import type { Lang } from "./copy";
import { useStoryEnv } from "./env";
import { HudFrame, StripFrame } from "./waveform/frames";
import { LevelStreamProvider, type Scenario } from "./waveform/stream";
import { VARIANTS, VARIANT_BY_ID, type VariantId, type WaveVariant } from "./waveform/variants";

const meta = {
  title: "Voice input/Waveform",
  id: "voice-input-waveform",
  parameters: { layout: "fullscreen" },
} satisfies Meta;
export default meta;
type Story = StoryObj<typeof meta>;

const SCENARIOS: { id: Scenario; label: Record<Lang, string> }[] = [
  { id: "speech", label: { zh: "正常说话", en: "Speaking" } },
  { id: "silence", label: { zh: "没出声", en: "Silent" } },
  { id: "loud", label: { zh: "大声", en: "Loud" } },
];

const TEXT = {
  compareTitle: { zh: "波形图：六个方向对比", en: "Waveform: six directions side by side" },
  compareLead: {
    zh: "所有方向读的是同一路输入，左边是输入框里的录音条，右边是按住说话时的浮层。工具栏「Voice」切到「My microphone」可以用你自己的声音。",
    en: "Every direction reads the same input. Left is the composer's recording strip, right is the push-to-talk pill. Switch Voice → My microphone in the toolbar to use your own voice.",
  },
  pause: { zh: "暂停", en: "Pause" },
  resume: { zh: "继续", en: "Resume" },
  restart: { zh: "重新开始", en: "Restart" },
  live: { zh: "实时", en: "Live" },
  stills: { zh: "静帧：三种输入", en: "Stills: three inputs" },
  mic: { zh: "正在使用你的麦克风。", en: "Using your microphone." },
  simulated: {
    zh: "当前是模拟说话人。工具栏「Voice」切到「My microphone」可以用真麦克风试。",
    en: "Using the simulated speaker. Switch Voice → My microphone in the toolbar to try your own voice.",
  },
};

function Controls({
  scenario,
  onScenario,
  paused,
  onPause,
  onRestart,
}: {
  scenario: Scenario;
  onScenario: (s: Scenario) => void;
  paused: boolean;
  onPause: () => void;
  onRestart: () => void;
}) {
  const { lang, source } = useStoryEnv();
  return (
    <div className="flex items-center gap-1">
      {source === "simulated" &&
        SCENARIOS.map((s) => (
          <Button
            key={s.id}
            size="xs"
            variant={scenario === s.id ? "secondary" : "ghost"}
            onClick={() => onScenario(s.id)}
          >
            {s.label[lang]}
          </Button>
        ))}
      <span className="mx-1 h-4 w-px bg-border" />
      <Button size="xs" variant="ghost" onClick={onPause}>
        {paused ? TEXT.resume[lang] : TEXT.pause[lang]}
      </Button>
      <Button size="xs" variant="ghost" onClick={onRestart}>
        {TEXT.restart[lang]}
      </Button>
    </div>
  );
}

function useLiveControls() {
  const [scenario, setScenario] = useState<Scenario>("speech");
  const [paused, setPaused] = useState(false);
  const [run, setRun] = useState(0);
  return {
    scenario,
    paused,
    run,
    controls: (
      <Controls
        scenario={scenario}
        onScenario={(s) => {
          setScenario(s);
          setPaused(false);
        }}
        paused={paused}
        onPause={() => setPaused((p) => !p)}
        onRestart={() => {
          setRun((r) => r + 1);
          setPaused(false);
        }}
      />
    ),
  };
}

function ComparePage() {
  const { lang } = useStoryEnv();
  const { scenario, paused, run, controls } = useLiveControls();
  return (
    <div className="min-h-screen bg-background p-10">
      <header className="flex items-end justify-between gap-8">
        <div className="max-w-2xl">
          <h1 className="text-title font-semibold">{TEXT.compareTitle[lang]}</h1>
          <p className="mt-1.5 text-body text-muted-foreground">{TEXT.compareLead[lang]}</p>
        </div>
        {controls}
      </header>
      <LevelStreamProvider key={run} scenario={scenario} paused={paused}>
        <div className="mt-8 divide-y divide-border">
          {VARIANTS.map((variant) => (
            <CompareRow key={variant.id} variant={variant} />
          ))}
        </div>
      </LevelStreamProvider>
    </div>
  );
}

function CompareRow({ variant }: { variant: WaveVariant }) {
  const { lang } = useStoryEnv();
  return (
    <section className="flex items-center gap-8 py-5">
      <div className="w-56 shrink-0">
        <div className="text-caption font-medium text-brand">{variant.letter}</div>
        <h2 className="text-body font-semibold">{variant.name[lang]}</h2>
        <p className="mt-0.5 text-caption text-muted-foreground">{variant.pitch[lang]}</p>
      </div>
      <StripFrame Wave={variant.Wave} className="-mx-5 -mb-3" />
      <HudFrame Wave={variant.Wave} />
    </section>
  );
}

function VariantPage({ id }: { id: VariantId }) {
  const { lang, source } = useStoryEnv();
  const variant = VARIANT_BY_ID[id];
  const { scenario, paused, run, controls } = useLiveControls();
  return (
    <div className="min-h-screen bg-background p-10">
      <div className="flex items-start gap-12">
        <div className="space-y-10">
          <section>
            <div className="mb-3 flex items-center justify-between">
              <h2 className="text-caption font-medium text-muted-foreground">{TEXT.live[lang]}</h2>
              {controls}
            </div>
            <LevelStreamProvider key={run} scenario={scenario} paused={paused}>
              <div className="flex flex-col items-center gap-4">
                <HudFrame Wave={variant.Wave} />
                <StripFrame Wave={variant.Wave} />
              </div>
            </LevelStreamProvider>
          </section>
          <section>
            <h2 className="mb-3 text-caption font-medium text-muted-foreground">{TEXT.stills[lang]}</h2>
            <div className="space-y-1">
              {SCENARIOS.map((s, index) => (
                <LevelStreamProvider key={s.id} scenario={s.id} frozen prefillMs={4050} seed={11 + index}>
                  <div className="flex items-center gap-4">
                    <span className="w-14 shrink-0 text-caption text-muted-foreground">{s.label[lang]}</span>
                    <StripFrame Wave={variant.Wave} className="-mx-5 -mb-3 w-[520px]" />
                    <HudFrame Wave={variant.Wave} />
                  </div>
                </LevelStreamProvider>
              ))}
            </div>
          </section>
        </div>
        <aside className="w-72 shrink-0 pt-1">
          <div className="text-caption font-medium text-brand">{variant.letter}</div>
          <h1 className="mt-1 text-title font-semibold">{variant.name[lang]}</h1>
          <p className="mt-2 text-body text-muted-foreground">{variant.pitch[lang]}</p>
          <ul className="mt-5 space-y-2 text-body">
            {variant.notes[lang].map((note) => (
              <li key={note} className="flex gap-2">
                <span className="mt-2 size-1 shrink-0 rounded-full bg-faint-foreground" />
                <span>{note}</span>
              </li>
            ))}
          </ul>
          <p className={cn("mt-6 text-caption text-faint-foreground")}>
            {source === "simulated" ? TEXT.simulated[lang] : TEXT.mic[lang]}
          </p>
        </aside>
      </div>
    </div>
  );
}

export const Compare: Story = { name: "Compare all", render: () => <ComparePage /> };
export const Current: Story = { name: "A · Current", render: () => <VariantPage id="current" /> };
export const SmoothScroll: Story = { name: "B · Smooth scroll", render: () => <VariantPage id="smooth" /> };
export const DotsToBars: Story = { name: "C · Dots to bars", render: () => <VariantPage id="dots" /> };
export const LiveSpectrum: Story = { name: "D · Live spectrum", render: () => <VariantPage id="spectrum" /> };
export const FlowingLine: Story = { name: "E · Flowing line", render: () => <VariantPage id="line" /> };
export const WholeRecording: Story = { name: "F · Whole recording", render: () => <VariantPage id="timeline" /> };
