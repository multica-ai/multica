import { act, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { DEFAULT_VOICE_SETTINGS, useVoiceModelsStore, useVoiceSettingsStore } from "@multica/core/voice";
import { renderWithI18n } from "../test/i18n";
import { SERVER_VOICE_MODEL } from "./engine";
import { VoiceModelRows } from "./voice-models";

vi.mock("./engine", async (importOriginal) => {
  const { useVoiceModelsStore } = await import("@multica/core/voice");
  return {
    ...(await importOriginal<typeof import("./engine")>()),
    useVoiceModels: () => useVoiceModelsStore((s) => s.models),
  };
});

beforeEach(() => {
  act(() => {
    useVoiceSettingsStore.getState().update(DEFAULT_VOICE_SETTINGS);
    useVoiceModelsStore.getState().setModels({ engineAvailable: true, models: [{ ...SERVER_VOICE_MODEL }] });
  });
});

describe("VoiceModelRows in the setup dialog", () => {
  it("lets the server model be chosen when a lost on-device choice left none in use", async () => {
    act(() => useVoiceSettingsStore.getState().update({ modelId: "deleted-local" }));
    renderWithI18n(<VoiceModelRows />);
    expect(screen.queryByText("In use")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Delete/ })).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Use" }));
    expect(useVoiceSettingsStore.getState().modelId).toBe(SERVER_VOICE_MODEL.id);
    expect(screen.getByText("In use")).toBeInTheDocument();
  });
});
