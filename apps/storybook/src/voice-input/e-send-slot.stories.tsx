import type { Meta, StoryObj } from "@storybook/react-vite";
import { SendSlot } from "./concepts/send-slot";
import { SEND_SLOT } from "./concepts/info";
import { useStoryEnv } from "./env";
import { Gallery, samples } from "./gallery";
import { label } from "./labels";
import { DemoStage } from "./stage";

const meta = { title: "Voice input/Explored/E · Hold the send button", id: "voice-input-e-send-slot" } satisfies Meta;
export default meta;

export const TryIt: StoryObj = {
  name: "Try it",
  render: () => (
    <DemoStage info={SEND_SLOT}>
      <SendSlot />
    </DemoStage>
  ),
};

function States() {
  const { lang, copy } = useStoryEnv();
  const s = samples(lang);
  return (
    <Gallery
      columns={3}
      items={[
        { label: label("idle", lang), node: <SendSlot frozen={s.idle} /> },
        { label: label("idleDraft", lang), node: <SendSlot frozen={s.idle} initialDraft={copy.userMessage} /> },
        { label: label("listening", lang), node: <SendSlot frozen={s.listening} /> },
        { label: label("cancelling", lang), node: <SendSlot frozen={s.listening} frozenDrag={{ dx: -70, dy: 0 }} /> },
        { label: label("locking", lang), node: <SendSlot frozen={s.listening} frozenDrag={{ dx: 0, dy: -40 }} /> },
        { label: label("locked", lang), node: <SendSlot frozen={s.listening} frozenLocked /> },
      ]}
    />
  );
}

export const AllStates: StoryObj = { name: "States", render: () => <States /> };
