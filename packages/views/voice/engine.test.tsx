import { renderHook, waitFor, act } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { configStore } from "@multica/core/config";
import { useVoiceModelsStore } from "@multica/core/voice";
import { SERVER_VOICE_MODEL, serverVoiceRuntime, useVoiceModels } from "./engine";

const createVoiceSession = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/api", () => ({ api: { createVoiceSession } }));

describe("serverVoiceRuntime", () => {
  it("streams rather than transcribing clips", () => {
    const runtime = serverVoiceRuntime();
    expect(runtime.stream).toBeTypeOf("function");
    expect(runtime.history).toBeUndefined();
  });

  it("has nothing to re-transcribe a clip with", async () => {
    await expect(
      serverVoiceRuntime().transcribe(new Blob(["clip"]), SERVER_VOICE_MODEL, { language: "en", hotwords: [] }),
    ).resolves.toEqual({ ok: false, reason: "engine-unavailable" });
  });
});

describe("useVoiceModels on the web", () => {
  it("connects the server engine once the config says the server transcribes", async () => {
    const { result } = renderHook(() => useVoiceModels());
    expect(result.current).toBeNull();

    act(() => configStore.getState().setVoiceTranscriptionEnabled(true));
    await waitFor(() =>
      expect(result.current).toEqual({ engineAvailable: true, models: [SERVER_VOICE_MODEL] }),
    );
    expect(useVoiceModelsStore.getState().models?.models[0]?.status).toBe("ready");
  });
});
