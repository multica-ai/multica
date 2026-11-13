import type { Meta, StoryObj } from "@storybook/react-vite";
import { CHAT_SCRIPT } from "./copy";
import { FocusMode } from "./concepts/focus-mode";
import { FOCUS_MODE } from "./concepts/info";
import { useStoryEnv } from "./env";
import { Gallery, samples } from "./gallery";
import { label } from "./labels";
import { DemoStage } from "./stage";

const meta = { title: "Voice input/Explored/D · Focus mode", id: "voice-input-d-focus-mode" } satisfies Meta;
export default meta;

export const TryIt: StoryObj = {
  name: "Try it",
  render: () => (
    <DemoStage info={FOCUS_MODE}>
      <FocusMode />
    </DemoStage>
  ),
};

function States() {
  const { lang } = useStoryEnv();
  const s = samples(lang);
  const script = CHAT_SCRIPT[lang];
  return (
    <Gallery
      columns={3}
      items={[
        { label: label("listeningEmpty", lang), node: <FocusMode frozen={s.listeningEmpty} initialOpen /> },
        { label: label("listening", lang), node: <FocusMode frozen={s.listening} initialOpen /> },
        { label: label("paused", lang), node: <FocusMode frozen={s.paused} initialOpen /> },
        { label: label("processing", lang), node: <FocusMode frozen={s.processing} initialOpen /> },
        {
          label: label("review", lang),
          node: (
            <FocusMode
              frozen={s.idle}
              initialOpen
              frozenReview={{ raw: script.phrases.join(""), polished: script.polished }}
            />
          ),
        },
        { label: label("noSpeech", lang), node: <FocusMode frozen={s.noSpeech} initialOpen /> },
      ]}
    />
  );
}

export const AllStates: StoryObj = { name: "States", render: () => <States /> };
