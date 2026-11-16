import type { ReactNode } from "react";
import { act, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { configStore } from "@multica/core/config";
import { DEFAULT_VOICE_SETTINGS, useVoiceModelsStore, useVoiceSettingsStore } from "@multica/core/voice";
import { renderWithI18n } from "../../test/i18n";
import { SERVER_VOICE_MODEL } from "../../voice/engine";
import { VoiceInputTab } from "./voice-input-tab";

vi.mock("../../navigation", () => ({
  AppLink: ({ href, children, ...props }: { href: string; children?: ReactNode }) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
  useOptionalNavigation: () => null,
}));
vi.mock("../../voice/engine", async (importOriginal) => {
  const { useVoiceModelsStore } = await import("@multica/core/voice");
  return {
    ...(await importOriginal<typeof import("../../voice/engine")>()),
    useVoiceModels: () => useVoiceModelsStore((s) => s.models),
  };
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
  act(() => configStore.getState().setVoiceTranscriptionEnabled(false));
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

  it("on the web, transcribed by the server: no models to manage and no local history", async () => {
    delete (window as unknown as { desktopAPI?: unknown }).desktopAPI;
    configStore.getState().setVoiceTranscriptionEnabled(true);
    renderWithI18n(<VoiceInputTab />);
    expect(screen.getByText(/speech-to-text service your Multica server/)).toBeInTheDocument();
    expect(screen.queryByText("History")).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole("switch", { name: "Voice input" }));
    expect(screen.queryByText("Speech model")).not.toBeInTheDocument();
    expect(await screen.findByText("Microphone")).toBeInTheDocument();
  });

  it("on desktop with the server model in use, says speech goes to the server and keeps it undeletable", async () => {
    configStore.getState().setVoiceTranscriptionEnabled(true);
    useVoiceSettingsStore.getState().update({ enabled: true });
    useVoiceModelsStore.getState().setModels({
      engineAvailable: true,
      models: [
        { ...SERVER_VOICE_MODEL },
        { ...SERVER_VOICE_MODEL, id: "whisper", name: "Whisper", streams: undefined, sizeBytes: 1_000_000 },
      ],
    });
    renderWithI18n(<VoiceInputTab />);
    expect(screen.getByText(/speech-to-text service your Multica server/)).toBeInTheDocument();
    expect(screen.getByText(/The server model streams speech/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Delete Server" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Delete Whisper" })).toBeInTheDocument();

    act(() => useVoiceSettingsStore.getState().update({ modelId: "whisper" }));
    expect(screen.getByText(/transcribed on this computer and never leaves it/)).toBeInTheDocument();
  });

  it("on desktop with no engine of its own, lists the server model without promising local ones", () => {
    configStore.getState().setVoiceTranscriptionEnabled(true);
    useVoiceSettingsStore.getState().update({ enabled: true });
    useVoiceModelsStore.getState().setModels({ engineAvailable: true, models: [{ ...SERVER_VOICE_MODEL }] });
    renderWithI18n(<VoiceInputTab />);
    expect(screen.getByText("Server")).toBeInTheDocument();
    expect(screen.queryByText(/run on this computer/)).not.toBeInTheDocument();
  });
});

