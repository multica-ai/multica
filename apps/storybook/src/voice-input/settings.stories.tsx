import type { ReactNode } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { useStoryEnv } from "./env";
import { Gallery } from "./gallery";
import { EngineCard, ShortcutCard, VoiceNotices, VoiceSettingsPage } from "./settings/voice-settings-page";
import { VoiceSettingsProvider, type VoiceSettings } from "./settings/store";

const meta = {
  title: "Voice input/Settings",
  id: "voice-input-settings",
  parameters: { layout: "fullscreen" },
} satisfies Meta;
export default meta;

export const Desktop: StoryObj = {
  render: () => (
    <VoiceSettingsProvider>
      <VoiceSettingsPage />
    </VoiceSettingsProvider>
  ),
};

export const Web: StoryObj = {
  render: () => (
    <VoiceSettingsProvider initial={{ platform: "web", engine: "cloud" }}>
      <VoiceSettingsPage />
    </VoiceSettingsProvider>
  ),
};

function Case({ initial, children }: { initial: Partial<VoiceSettings>; children: ReactNode }) {
  return (
    <VoiceSettingsProvider initial={initial}>
      <div className="w-[620px] space-y-3">{children}</div>
    </VoiceSettingsProvider>
  );
}

function SettingsStates() {
  const { lang } = useStoryEnv();
  const zh = lang === "zh";
  return (
    <Gallery
      columns={2}
      items={[
        {
          label: zh ? "本机引擎，模型还没下载" : "On-device engine, no model yet",
          node: (
            <Case initial={{ models: { "sense-voice": { state: "available" } } }}>
              <VoiceNotices />
            </Case>
          ),
        },
        {
          label: zh ? "没有麦克风权限" : "Microphone permission denied",
          node: (
            <Case initial={{ micPermission: "denied" }}>
              <VoiceNotices />
            </Case>
          ),
        },
        {
          label: zh ? "模型下载中" : "Model downloading",
          node: (
            <Case initial={{ models: { "parakeet-v3": { state: "downloading", progress: 42 }, "whisper-turbo": { state: "ready" } } }}>
              <EngineCard />
            </Case>
          ),
        },
        {
          label: zh ? "云端引擎" : "Cloud engine",
          node: (
            <Case initial={{ engine: "cloud" }}>
              <EngineCard />
            </Case>
          ),
        },
        {
          label: zh ? "仅按住模式" : "Hold-only behavior",
          node: (
            <Case initial={{ activation: "hold", globalShortcut: true }}>
              <ShortcutCard />
            </Case>
          ),
        },
        {
          label: zh ? "网页端：只有云端" : "Web: cloud only",
          node: (
            <Case initial={{ platform: "web", engine: "cloud" }}>
              <VoiceNotices />
              <EngineCard />
            </Case>
          ),
        },
      ]}
    />
  );
}

export const States: StoryObj = { render: () => <SettingsStates /> };
