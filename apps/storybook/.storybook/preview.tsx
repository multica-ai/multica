import { useLayoutEffect, type ReactNode } from "react";
import type { Preview } from "@storybook/react-vite";
import { TooltipProvider } from "@multica/ui/components/ui/tooltip";
import { StoryEnvProvider } from "../src/voice-input/env";
import type { Lang } from "../src/voice-input/copy";
import type { VoiceSource } from "../src/voice-input/voice-session";
import "./styles.css";

function DocumentSync({ theme, lang, children }: { theme: string; lang: Lang; children: ReactNode }) {
  useLayoutEffect(() => {
    document.documentElement.classList.toggle("dark", theme === "dark");
    document.documentElement.lang = lang === "zh" ? "zh-CN" : "en";
  }, [theme, lang]);
  return children;
}

const preview: Preview = {
  globalTypes: {
    theme: {
      description: "Theme",
      toolbar: {
        title: "Theme",
        icon: "mirror",
        items: [
          { value: "light", title: "Light" },
          { value: "dark", title: "Dark" },
        ],
        dynamicTitle: true,
      },
    },
    lang: {
      description: "Copy and sample speech",
      toolbar: {
        title: "Language",
        icon: "globe",
        items: [
          { value: "zh", title: "中文" },
          { value: "en", title: "English" },
        ],
        dynamicTitle: true,
      },
    },
    voice: {
      description: "Where the voice comes from",
      toolbar: {
        title: "Voice",
        icon: "speaker",
        items: [
          { value: "simulated", title: "Simulated speaker" },
          { value: "microphone", title: "My microphone" },
        ],
        dynamicTitle: true,
      },
    },
  },
  initialGlobals: { theme: "light", lang: "zh", voice: "simulated" },
  parameters: {
    layout: "centered",
    options: {
      storySort: { order: ["Comment box", ["Compare all", "*"], "Voice input", ["Composer (A + C)", "Waveform", ["Compare all", "*"], "Settings", "First use", "Edge cases", "Explored", ["All directions", "*"]]] },
    },
    controls: { disable: true },
    actions: { disable: true },
  },
  decorators: [
    (Story, context) => {
      const lang = context.globals.lang as Lang;
      return (
        <DocumentSync theme={context.globals.theme as string} lang={lang}>
          <StoryEnvProvider lang={lang} source={context.globals.voice as VoiceSource}>
            <TooltipProvider>
              <div className="font-sans text-foreground antialiased">
                <Story />
              </div>
            </TooltipProvider>
          </StoryEnvProvider>
        </DocumentSync>
      );
    },
  ],
};

export default preview;
