import type { Meta, StoryObj } from "@storybook/react-vite";
import { VoiceToIssue } from "./concepts/voice-to-issue";
import { VOICE_TO_ISSUE } from "./concepts/info";
import { useStoryEnv } from "./env";
import { Gallery, issueSamples } from "./gallery";
import { label } from "./labels";
import { DemoStage } from "./stage";

const meta = { title: "Voice input/F · Voice to issue", id: "voice-input-f-voice-to-issue" } satisfies Meta;
export default meta;

export const TryIt: StoryObj = {
  name: "Try it",
  render: () => (
    <DemoStage info={VOICE_TO_ISSUE}>
      <VoiceToIssue />
    </DemoStage>
  ),
};

function States() {
  const { lang } = useStoryEnv();
  const s = issueSamples(lang);
  return (
    <Gallery
      columns={2}
      items={[
        { label: label("idle", lang), node: <VoiceToIssue frozen={s.idle} /> },
        { label: label("listening", lang), node: <VoiceToIssue frozen={s.listening} /> },
        { label: label("processing", lang), node: <VoiceToIssue frozen={s.processing} /> },
        { label: label("filled", lang), node: <VoiceToIssue frozen={s.idle} frozenStep={3} /> },
      ]}
    />
  );
}

export const AllStates: StoryObj = { name: "States", render: () => <States /> };
