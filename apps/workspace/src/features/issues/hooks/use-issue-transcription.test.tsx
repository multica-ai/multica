import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useIssueTranscription } from "./use-issue-transcription";
import { api } from "@/shared/api";

vi.mock("@/shared/api", () => ({
  api: {
    transcribeAudio: vi.fn(),
  },
}));

describe("useIssueTranscription", () => {
  beforeEach(() => {
    vi.mocked(api.transcribeAudio).mockReset();
  });

  it("returns transcript results", async () => {
    vi.mocked(api.transcribeAudio).mockResolvedValue({
      text: "hello",
      provider: "cloudflare",
      model: "@cf/openai/whisper-large-v3-turbo",
    });

    const { result } = renderHook(() => useIssueTranscription());
    const file = new File(["audio"], "voice.webm", { type: "audio/webm" });

    await act(async () => {
      await result.current.transcribe(file);
    });

    expect(api.transcribeAudio).toHaveBeenCalledWith(file);
    expect(result.current.status).toBe("success");
    expect(result.current.result?.text).toBe("hello");
  });

  it("stores normalized errors", async () => {
    vi.mocked(api.transcribeAudio).mockRejectedValue(new Error("provider disabled"));

    const { result } = renderHook(() => useIssueTranscription());
    const file = new File(["audio"], "voice.webm", { type: "audio/webm" });

    await act(async () => {
      await result.current.transcribe(file);
    });

    expect(result.current.status).toBe("error");
    expect(result.current.error).toBe("provider disabled");
  });
});
