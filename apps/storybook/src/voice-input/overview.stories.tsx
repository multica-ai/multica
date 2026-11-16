import type { ReactNode } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { FocusMode } from "./concepts/focus-mode";
import { ALL_CONCEPTS } from "./concepts/info";
import { InlineWaveform } from "./concepts/inline-waveform";
import { LiveDictation } from "./concepts/live-dictation";
import { PushToTalk } from "./concepts/push-to-talk";
import { SendSlot } from "./concepts/send-slot";
import { useStoryEnv } from "./env";
import { samples } from "./gallery";
import type { ConceptInfo } from "./stage";

const meta = {
  title: "Voice input/Explored/All directions",
  id: "voice-input-overview",
  parameters: { layout: "fullscreen" },
} satisfies Meta;
export default meta;

function Card({ info, children }: { info: ConceptInfo; children: ReactNode }) {
  const { lang } = useStoryEnv();
  return (
    <section className="flex flex-col gap-4 rounded-xl bg-surface p-5 ring-1 ring-surface-border">
      <header>
        <div className="flex items-baseline gap-2">
          <span className="text-caption font-medium text-brand">{info.letter}</span>
          <h2 className="text-title-sm font-semibold">{info.name[lang]}</h2>
        </div>
        <p className="mt-1 text-caption text-muted-foreground">{info.pitch[lang]}</p>
      </header>
      <div className="flex h-[420px] items-center justify-center overflow-hidden rounded-lg bg-page-canvas">
        <div style={{ zoom: 0.68 }}>{children}</div>
      </div>
      <dl className="grid grid-cols-2 gap-3 text-caption">
        <div>
          <dt className="font-medium">{lang === "zh" ? "适合" : "Best for"}</dt>
          <dd className="mt-0.5 text-muted-foreground">{info.bestFor[lang]}</dd>
        </div>
        <div>
          <dt className="font-medium">{lang === "zh" ? "代价" : "Trade-off"}</dt>
          <dd className="mt-0.5 text-muted-foreground">{info.tradeoff[lang]}</dd>
        </div>
      </dl>
    </section>
  );
}

function Overview() {
  const { lang } = useStoryEnv();
  const s = samples(lang);
  const renders = [
    <InlineWaveform key="a" frozen={s.listening} />,
    <LiveDictation key="b" frozen={s.listening} />,
    <PushToTalk key="c" frozen={s.listening} />,
    <FocusMode key="d" frozen={s.listening} initialOpen />,
    <SendSlot key="e" frozen={s.listening} />,
  ];
  return (
    <div className="min-h-screen bg-page-canvas p-8">
      <header className="mb-6 max-w-3xl">
        <h1 className="text-display-sm font-semibold">{lang === "zh" ? "第一轮探索：五种方向" : "First round: five directions"}</h1>
        <p className="mt-2 text-body text-muted-foreground">
          {lang === "zh"
            ? "每个方向都定格在「正在听」这一刻。最终选了 A + C，见「Composer (A + C)」。这里保留探索过程，方便回看。"
            : "Each direction is frozen mid-dictation. A + C were picked; see “Composer (A + C)”. The exploration stays here for reference."}
        </p>
      </header>
      <div className="grid grid-cols-3 gap-6">
        {ALL_CONCEPTS.map((info, i) => (
          <Card key={info.letter} info={info}>
            {renders[i]}
          </Card>
        ))}
      </div>
    </div>
  );
}

export const AllDirections: StoryObj = { name: "Overview", render: () => <Overview /> };
