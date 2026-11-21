import type { Meta, StoryObj } from "@storybook/react-vite";
import { InlineWaveform } from "./concepts/inline-waveform";
import { INLINE_WAVEFORM } from "./concepts/info";
import { useStoryEnv } from "./env";
import { Gallery, samples } from "./gallery";
import { label } from "./labels";
import { DemoStage } from "./stage";

const meta = { title: "Voice input/Explored/A · Inline waveform", id: "voice-input-a-inline-waveform" } satisfies Meta;
export default meta;

export const TryIt: StoryObj = {
  name: "Try it",
  render: () => (
    <DemoStage info={INLINE_WAVEFORM}>
      <InlineWaveform />
    </DemoStage>
  ),
};

function States() {
  const { lang, copy } = useStoryEnv();
  const s = samples(lang);
  return (
    <Gallery
      columns={2}
      items={[
        { label: label("idle", lang), node: <InlineWaveform frozen={s.idle} framed={false} /> },
        { label: label("requesting", lang), node: <InlineWaveform frozen={s.requesting} framed={false} /> },
        { label: label("listening", lang), node: <InlineWaveform frozen={s.listening} framed={false} initialDraft={copy.userMessage} /> },
        { label: label("processing", lang), node: <InlineWaveform frozen={s.processing} framed={false} initialDraft={copy.userMessage} /> },
        { label: label("noSpeech", lang), node: <InlineWaveform frozen={s.noSpeech} framed={false} /> },
        { label: label("denied", lang), node: <InlineWaveform frozen={s.denied} framed={false} /> },
      ]}
    />
  );
}

export const AllStates: StoryObj = { name: "States", render: () => <States /> };
