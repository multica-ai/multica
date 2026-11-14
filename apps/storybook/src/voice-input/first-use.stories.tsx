import { useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { ChatWindowFrame, useDemoChat } from "./chat-frame";
import { useStoryEnv } from "./env";
import { Gallery, samples } from "./gallery";
import { ChatComposer } from "./composer/surfaces";
import { VoiceProvider } from "./composer/voice-provider";
import { FirstUseDialog, type SetupStep } from "./setup/first-use";
import { VoiceSettingsProvider } from "./settings/store";

const meta = {
  title: "Voice input/First use",
  id: "voice-input-first-use",
  parameters: { layout: "fullscreen" },
} satisfies Meta;
export default meta;

/** The first press on the mic opens setup; afterwards it records straight away. */
function FirstPress() {
  const chat = useDemoChat();
  const [setupDone, setSetupDone] = useState(false);
  const [open, setOpen] = useState(false);
  return (
    <div className="relative flex min-h-screen items-start justify-center p-8">
      <VoiceProvider
        primaryId="chat"
        gate={() => {
          if (setupDone) return true;
          setOpen(true);
          return false;
        }}
      >
        <ChatWindowFrame messages={chat.messages}>
          <ChatComposer onSend={chat.send} />
        </ChatWindowFrame>
      </VoiceProvider>
      {open && (
        <div className="absolute inset-0 z-50 flex items-center justify-center bg-black/30 animate-in fade-in">
          <FirstUseDialog
            onClose={() => {
              setOpen(false);
              setSetupDone(true);
            }}
          />
        </div>
      )}
    </div>
  );
}

export const TryIt: StoryObj = {
  name: "Try it",
  render: () => (
    <VoiceSettingsProvider initial={{ micPermission: "prompt", models: { "sense-voice": { state: "available" }, "parakeet-v3": { state: "available" } } }}>
      <FirstPress />
    </VoiceSettingsProvider>
  ),
};

function StepsGallery() {
  const { lang } = useStoryEnv();
  const zh = lang === "zh";
  const s = samples(lang);
  const steps: { step: SetupStep; label: string }[] = [
    { step: "permission", label: zh ? "1 · 麦克风权限" : "1 · Microphone permission" },
    { step: "engine", label: zh ? "2 · 选择识别方式" : "2 · Choose the engine" },
    { step: "try", label: zh ? "3 · 试一下（按住时）" : "3 · Try it (holding)" },
    { step: "done", label: zh ? "4 · 完成" : "4 · Done" },
  ];
  return (
    <Gallery
      columns={2}
      items={steps.map(({ step, label }) => ({
        label,
        node: (
          <VoiceSettingsProvider initial={{ models: { "sense-voice": { state: "ready" }, "parakeet-v3": { state: "ready" } } }}>
            <FirstUseDialog initialStep={step} frozen={step === "try" ? s.listening : undefined} />
          </VoiceSettingsProvider>
        ),
      }))}
    />
  );
}

export const AllSteps: StoryObj = { name: "All steps", render: () => <StepsGallery /> };
