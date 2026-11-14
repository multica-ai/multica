import type { Meta, StoryObj } from "@storybook/react-vite";
import { PushToTalk } from "./concepts/push-to-talk";
import { PUSH_TO_TALK } from "./concepts/info";
import { useStoryEnv } from "./env";
import { Gallery, samples } from "./gallery";
import { label } from "./labels";
import { DemoStage } from "./stage";

const meta = { title: "Voice input/Explored/C · Push-to-talk", id: "voice-input-c-push-to-talk" } satisfies Meta;
export default meta;

export const TryIt: StoryObj = {
  name: "Try it",
  render: () => (
    <DemoStage info={PUSH_TO_TALK}>
      <PushToTalk />
    </DemoStage>
  ),
};

function States() {
  const { lang } = useStoryEnv();
  const s = samples(lang);
  return (
    <Gallery
      columns={3}
      items={[
        { label: label("idle", lang), node: <PushToTalk frozen={s.idle} /> },
        { label: label("listening", lang), node: <PushToTalk frozen={s.listening} /> },
        { label: label("locked", lang), node: <PushToTalk frozen={s.listening} frozenLocked /> },
        { label: label("processing", lang), node: <PushToTalk frozen={s.processing} /> },
        { label: label("noSpeech", lang), node: <PushToTalk frozen={s.noSpeech} /> },
      ]}
    />
  );
}

export const AllStates: StoryObj = { name: "States", render: () => <States /> };
