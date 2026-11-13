import type { Meta, StoryObj } from "@storybook/react-vite";
import { CHAT_SCRIPT } from "./copy";
import { LiveDictation } from "./concepts/live-dictation";
import { LIVE_DICTATION } from "./concepts/info";
import { useStoryEnv } from "./env";
import { Gallery, samples } from "./gallery";
import { label } from "./labels";
import { DemoStage } from "./stage";

const meta = { title: "Voice input/Explored/B · Live dictation", id: "voice-input-b-live-dictation" } satisfies Meta;
export default meta;

export const TryIt: StoryObj = {
  name: "Try it",
  render: () => (
    <DemoStage info={LIVE_DICTATION}>
      <LiveDictation />
    </DemoStage>
  ),
};

function States() {
  const { lang } = useStoryEnv();
  const s = samples(lang);
  return (
    <Gallery
      columns={2}
      items={[
        { label: label("idle", lang), node: <LiveDictation frozen={s.idle} framed={false} /> },
        { label: label("requesting", lang), node: <LiveDictation frozen={s.requesting} framed={false} /> },
        { label: label("listening", lang), node: <LiveDictation frozen={s.listening} framed={false} /> },
        {
          label: label("polish", lang),
          node: <LiveDictation frozen={s.idle} framed={false} initialDraft={CHAT_SCRIPT[lang].phrases.join("")} showPolish />,
        },
        { label: label("noSpeech", lang), node: <LiveDictation frozen={s.noSpeech} framed={false} /> },
        { label: label("network", lang), node: <LiveDictation frozen={s.network} framed={false} /> },
      ]}
    />
  );
}

export const AllStates: StoryObj = { name: "States", render: () => <States /> };
