import type { Meta, StoryObj } from "@storybook/react-vite";
import { ChatComposer } from "./composer/surfaces";
import { VoiceProvider } from "./composer/voice-provider";
import type { VoiceSessionOptions } from "./voice-session";
import { useStoryEnv } from "./env";
import { label, type StateLabel } from "./labels";

const meta = {
  title: "Voice input/Edge cases",
  parameters: { layout: "centered" },
} satisfies Meta;
export default meta;

/**
 * The failure paths on the shared composer. The error sits inside whichever
 * composer was dictated into; the draft is never touched.
 */
function Case({ state, simulate, note }: {
  state: StateLabel;
  simulate: Pick<VoiceSessionOptions, "failWith" | "permission">;
  note: { en: string; zh: string };
}) {
  const { lang } = useStoryEnv();
  return (
    <div className="flex w-[460px] flex-col gap-3 p-8">
      <div>
        <h1 className="text-title-sm font-semibold">{label(state, lang)}</h1>
        <p className="mt-1 text-caption text-muted-foreground">{note[lang]}</p>
      </div>
      <VoiceProvider primaryId="chat" simulate={simulate}>
        <div className="pt-14">
          <ChatComposer onSend={() => {}} />
        </div>
      </VoiceProvider>
    </div>
  );
}

export const FirstUse: StoryObj = {
  name: "First use: permission prompt",
  render: () => (
    <Case
      state="requesting"
      simulate={{ permission: "prompt" }}
      note={{
        en: "Click the mic. The first start waits on the browser's permission prompt; later starts go straight to listening.",
        zh: "点麦克风。第一次会等待浏览器弹出的权限请求；之后再点会直接开始听。",
      }}
    />
  ),
};

export const Blocked: StoryObj = {
  name: "Microphone blocked",
  render: () => (
    <Case
      state="denied"
      simulate={{ failWith: "permission-denied" }}
      note={{
        en: "Click the mic. Permission is refused; the composer says how to fix it and offers no retry that would fail again.",
        zh: "点麦克风。权限被拒绝；输入框里说明怎么解决，不提供注定失败的重试。",
      }}
    />
  ),
};

export const NothingHeard: StoryObj = {
  name: "Nothing heard",
  render: () => (
    <Case
      state="noSpeech"
      simulate={{ failWith: "no-speech" }}
      note={{
        en: "Click the mic and wait. After a few seconds of silence the session ends on its own.",
        zh: "点麦克风后不说话。静音几秒后自动结束。",
      }}
    />
  ),
};

export const TranscriptionFails: StoryObj = {
  name: "Transcription fails",
  render: () => (
    <Case
      state="network"
      simulate={{ failWith: "network" }}
      note={{
        en: "Click the mic, let it talk, then ✓. Transcription fails; the recording is kept so Try again doesn't make you repeat yourself.",
        zh: "点麦克风，等它说一会儿，再点 ✓。转写失败；录音保留，「重试」不需要再说一遍。",
      }}
    />
  ),
};
