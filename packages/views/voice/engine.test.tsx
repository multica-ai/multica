import { renderHook, waitFor, act } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { configStore } from "@multica/core/config";
import { useVoiceModelsStore, useVoiceSettingsStore } from "@multica/core/voice";
import type { VoiceModels } from "@multica/core/voice";
import { SERVER_VOICE_MODEL, desktopVoiceRuntime, serverVoiceRuntime, useVoiceModels } from "./engine";

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

describe("desktopVoiceRuntime", () => {
  const LOCAL: VoiceModels = {
    engineAvailable: true,
    models: [{ ...SERVER_VOICE_MODEL, id: "whisper", name: "Whisper", streams: undefined }],
  };

  function withDesktop(run: () => Promise<void>) {
    const voice = {
      listModels: vi.fn(() => Promise.resolve(LOCAL)),
      onModelsChanged: vi.fn(() => () => {}),
      transcribe: vi.fn(),
      prepare: vi.fn(),
    };
    (window as unknown as { desktopAPI?: unknown }).desktopAPI = { voice };
    return run().finally(() => {
      delete (window as unknown as { desktopAPI?: unknown }).desktopAPI;
      configStore.getState().setVoiceTranscriptionEnabled(false);
    });
  }

  it("offers only on-device models while the server does not transcribe", () =>
    withDesktop(async () => {
      configStore.getState().setVoiceTranscriptionEnabled(false);
      const onChange = vi.fn();
      desktopVoiceRuntime()!.watchModels(onChange);
      await waitFor(() => expect(onChange).toHaveBeenLastCalledWith(LOCAL));
    }));

  it("leads with the server model once the server transcribes, and can stream", () =>
    withDesktop(async () => {
      configStore.getState().setVoiceTranscriptionEnabled(false);
      const onChange = vi.fn();
      const runtime = desktopVoiceRuntime()!;
      runtime.watchModels(onChange);
      await waitFor(() => expect(onChange).toHaveBeenLastCalledWith(LOCAL));
      act(() => configStore.getState().setVoiceTranscriptionEnabled(true));
      expect(onChange).toHaveBeenLastCalledWith({
        engineAvailable: true,
        models: [SERVER_VOICE_MODEL, ...LOCAL.models],
      });
      expect(runtime.stream).toBeTypeOf("function");
    }));

  it("offers the server model at once when the local engine never answers", () =>
    withDesktop(async () => {
      (window as unknown as { desktopAPI: { voice: { listModels: () => Promise<never> } } }).desktopAPI.voice.listModels =
        () => new Promise<never>(() => {});
      configStore.getState().setVoiceTranscriptionEnabled(true);
      const onChange = vi.fn();
      desktopVoiceRuntime()!.watchModels(onChange);
      expect(onChange).toHaveBeenLastCalledWith({ engineAvailable: true, models: [SERVER_VOICE_MODEL] });
    }));

  it("waits for the engine before listing anything when an on-device model was chosen", () =>
    withDesktop(async () => {
      (window as unknown as { desktopAPI: { voice: { listModels: () => Promise<VoiceModels> } } }).desktopAPI.voice.listModels =
        () => new Promise<VoiceModels>((resolve) => setTimeout(() => resolve(LOCAL), 20));
      useVoiceSettingsStore.getState().update({ modelId: "whisper" });
      configStore.getState().setVoiceTranscriptionEnabled(true);
      const onChange = vi.fn();
      desktopVoiceRuntime()!.watchModels(onChange);
      expect(onChange).not.toHaveBeenCalled();
      await waitFor(() =>
        expect(onChange).toHaveBeenLastCalledWith({ engineAvailable: true, models: [SERVER_VOICE_MODEL, ...LOCAL.models] }),
      );
    }).finally(() => useVoiceSettingsStore.getState().update({ modelId: null })));

  it("lists only the server model where this computer has no engine", () =>
    withDesktop(async () => {
      (window as unknown as { desktopAPI: { voice: { listModels: () => Promise<VoiceModels> } } }).desktopAPI.voice.listModels =
        () => Promise.resolve({ ...LOCAL, engineAvailable: false });
      configStore.getState().setVoiceTranscriptionEnabled(true);
      const onChange = vi.fn();
      desktopVoiceRuntime()!.watchModels(onChange);
      await new Promise((resolve) => setTimeout(resolve, 0));
      expect(onChange).toHaveBeenLastCalledWith({ engineAvailable: true, models: [SERVER_VOICE_MODEL] });
    }));
});
