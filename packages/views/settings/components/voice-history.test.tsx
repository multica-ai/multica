import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { DEFAULT_VOICE_SETTINGS, useVoiceModelsStore, useVoiceSettingsStore, type VoiceHistoryEntry } from "@multica/core/voice";
import { renderWithI18n } from "../../test/i18n";
import { VoiceHistorySection } from "./voice-history";

const transcribe = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/voice", async (original) => ({
  ...(await original<typeof import("@multica/core/voice")>()),
  transcribeVoiceClip: transcribe,
}));
vi.mock("../../voice/engine", async () => {
  const { useVoiceModelsStore } = await import("@multica/core/voice");
  return { useVoiceModels: () => useVoiceModelsStore((s) => s.models) };
});

const now = Date.now();
let entries: VoiceHistoryEntry[];
const api = {
  list: vi.fn(async () => entries),
  onChanged: vi.fn(() => () => {}),
  save: vi.fn(),
  update: vi.fn(async () => {}),
  audio: vi.fn(async () => ({ audio: new Uint8Array([1]), mimeType: "audio/webm" })),
  remove: vi.fn(async () => {}),
  setRetention: vi.fn(async () => {}),
  clear: vi.fn(async () => {}),
};

beforeEach(() => {
  entries = [
    { id: "a", createdAt: now, durationMs: 4200, status: "done", text: "今天的第一条" },
    { id: "b", createdAt: now - 60_000, durationMs: 9000, status: "failed", text: "" },
    { id: "c", createdAt: now - 3 * 24 * 60 * 60 * 1000, durationMs: 1000, status: "interrupted", text: "" },
  ];
  Object.values(api).forEach((fn) => fn.mockClear());
  transcribe.mockReset();
  useVoiceSettingsStore.getState().update(DEFAULT_VOICE_SETTINGS);
  useVoiceModelsStore.getState().setModels({
    engineAvailable: true,
    models: [{ id: "m", name: "M", author: "A", licenseUrl: "", languages: ["zh"], languageHint: true,
      sizeBytes: 1, status: "ready", receivedBytes: 1 }],
  });
  (window as unknown as { desktopAPI: unknown }).desktopAPI = {
    voice: { listModels: vi.fn(), onModelsChanged: vi.fn(), transcribe: vi.fn(), history: api },
  };
});

afterEach(() => {
  delete (window as unknown as { desktopAPI?: unknown }).desktopAPI;
});

describe("VoiceHistorySection", () => {
  it("lists dictations by day, with what happened to the ones without text", async () => {
    renderWithI18n(<VoiceHistorySection />);
    expect(await screen.findByText("今天的第一条")).toBeInTheDocument();
    expect(screen.getByText("Today")).toBeInTheDocument();
    expect(screen.getByText("Transcription failed")).toBeInTheDocument();
    expect(screen.getByText("Interrupted before it was transcribed")).toBeInTheDocument();
    expect(screen.getByText("0:04")).toBeInTheDocument();
  });

  it("transcribes a failed recording again from its audio", async () => {
    transcribe.mockResolvedValue("找回来了");
    renderWithI18n(<VoiceHistorySection />);
    await screen.findByText("Transcription failed");
    await act(async () => fireEvent.click(screen.getAllByRole("button", { name: "Transcribe again" })[1]!));
    expect(api.update).toHaveBeenNthCalledWith(1, "b", { status: "transcribing" });
    expect(api.audio).toHaveBeenCalledWith("b");
    await waitFor(() => expect(api.update).toHaveBeenLastCalledWith("b", { status: "done", text: "找回来了" }));
  });

  it("applies the new retention when it changes", async () => {
    const user = userEvent.setup();
    renderWithI18n(<VoiceHistorySection />);
    await screen.findByText("今天的第一条");
    await user.click(screen.getByRole("combobox", { name: "Keep history" }));
    await user.click(await screen.findByRole("option", { name: "7 days" }));
    expect(useVoiceSettingsStore.getState().keepHistory).toBe("7d");
    expect(api.setRetention).toHaveBeenCalledWith(7);
  });

  it("clears everything after confirming, without changing the retention", async () => {
    renderWithI18n(<VoiceHistorySection />);
    await screen.findByText("今天的第一条");
    fireEvent.click(screen.getByRole("button", { name: "Clear all" }));
    fireEvent.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Clear all" }));
    expect(api.clear).toHaveBeenCalledOnce();
    expect(api.setRetention).not.toHaveBeenCalled();
    expect(useVoiceSettingsStore.getState().keepHistory).toBe("30d");
  });
});
