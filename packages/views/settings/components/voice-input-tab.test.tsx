import type { ReactNode } from "react";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { DEFAULT_VOICE_SETTINGS, useVoiceModelsStore, useVoiceSettingsStore } from "@multica/core/voice";
import { renderWithI18n } from "../../test/i18n";
import { VoiceInputTab } from "./voice-input-tab";

vi.mock("../../navigation", () => ({
  AppLink: ({ href, children, ...props }: { href: string; children?: ReactNode }) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
  useOptionalNavigation: () => null,
}));
vi.mock("../../voice/engine", async () => {
  const { useVoiceModelsStore } = await import("@multica/core/voice");
  return { useVoiceModels: () => useVoiceModelsStore((s) => s.models) };
});

beforeEach(() => {
  useVoiceSettingsStore.getState().update(DEFAULT_VOICE_SETTINGS);
  useVoiceModelsStore.getState().setModels({ engineAvailable: true, models: [] });
  (window as unknown as { desktopAPI: unknown }).desktopAPI = {
    voice: {
      listModels: vi.fn(),
      onModelsChanged: vi.fn(),
      transcribe: vi.fn(),
      history: {
        list: vi.fn(async () => []),
        onChanged: vi.fn(() => () => {}),
        setRetention: vi.fn(async () => {}),
      },
    },
  };
});

afterEach(() => {
  delete (window as unknown as { desktopAPI?: unknown }).desktopAPI;
});

describe("VoiceInputTab", () => {
  it("starts off with only the switch and history, and turning it on shows the rest", async () => {
    renderWithI18n(<VoiceInputTab />);
    const toggle = screen.getByRole("switch", { name: "Voice input" });
    expect(toggle).not.toBeChecked();
    expect(screen.queryByText("Speech model")).not.toBeInTheDocument();
    expect(screen.queryByText("Microphone")).not.toBeInTheDocument();
    expect(screen.getByText("History")).toBeInTheDocument();

    await userEvent.click(toggle);
    expect(useVoiceSettingsStore.getState().enabled).toBe(true);
    expect(screen.getByRole("switch", { name: "Voice input" })).toBeChecked();
    expect(screen.getByText("Speech model")).toBeInTheDocument();
  });
});
