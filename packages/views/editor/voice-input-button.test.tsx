import { act, cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { DictationProvider } from "@multica/core/platform/dictation";
import type { DictationAdapter } from "@multica/core/types/dictation";
import { renderWithI18n } from "../test/i18n";
import type { ContentEditorRef } from "./content-editor";
import { VoiceInputButton } from "./voice-input-button";

const mocks = vi.hoisted(() => ({ toastInfo: vi.fn(), toastError: vi.fn() }));
vi.mock("sonner", () => ({ toast: { info: mocks.toastInfo, error: mocks.toastError } }));
const getUserMedia = vi.fn();
const fetchSpy = vi.fn();

function editorRef() {
  return {
    ref: {
      current: { focusForNativeInput: vi.fn(() => true) } as unknown as ContentEditorRef,
    },
    insert: vi.fn(),
  };
}

describe("VoiceInputButton native-only bridge", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.stubGlobal("fetch", fetchSpy);
    Object.defineProperty(navigator, "mediaDevices", {
      value: { getUserMedia }, configurable: true,
    });
  });
  afterEach(() => {
    cleanup();
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("renders no mic without a host adapter", () => {
    renderWithI18n(<VoiceInputButton editorRef={editorRef().ref} />);
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    expect(getUserMedia).not.toHaveBeenCalled();
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  function nativeButton(
    ref: { current: ContentEditorRef | null },
    adapter: DictationAdapter,
    props: { disabled?: boolean; onBeforeRecord?: () => void } = {},
  ) {
    return (
      <DictationProvider adapter={adapter}>
        <VoiceInputButton editorRef={ref} {...props} />
      </DictationProvider>
    );
  }

  it("delegates to native dictation without capturing, fetching, inserting, or submitting", async () => {
    const { ref, insert } = editorRef();
    const toggle = vi.fn().mockResolvedValue({ ok: true, shortcut: "Ctrl+Alt+Shift+D" });
    const submit = vi.fn((event) => event.preventDefault());
    renderWithI18n(<form onSubmit={submit}>{nativeButton(ref, { toggle })}</form>);

    fireEvent.click(screen.getByRole("button", { name: "Dictate with Codex" }));
    await waitFor(() => expect(toggle).toHaveBeenCalledExactlyOnceWith());
    expect(ref.current.focusForNativeInput).toHaveBeenCalledOnce();
    expect(mocks.toastInfo).toHaveBeenCalledWith(expect.stringContaining("Sent Ctrl+Alt+Shift+D"));
    expect(submit).not.toHaveBeenCalled();
    expect(insert).not.toHaveBeenCalled();
    expect(getUserMedia).not.toHaveBeenCalled();
    expect(fetchSpy).not.toHaveBeenCalled();
    // Native recording state is unknown; don't invent a pressed/recording UI.
    expect(screen.getByRole("button", { name: "Dictate with Codex" })).not.toHaveAttribute("aria-pressed");
  });

  it.each(["not_configured", "app_not_running", "not_focused", "busy", "cleanup_failed", "unavailable"])(
    "never falls back to API or microphone capture on native %s", async (reason) => {
      const { ref, insert } = editorRef();
      const toggle = vi.fn().mockResolvedValue({ ok: false, reason });
      renderWithI18n(nativeButton(ref, { toggle }));
      fireEvent.click(screen.getByRole("button", { name: "Dictate with Codex" }));
      await waitFor(() => expect(mocks.toastError).toHaveBeenCalledOnce());
      expect(getUserMedia).not.toHaveBeenCalled();
      expect(fetchSpy).not.toHaveBeenCalled();
      expect(insert).not.toHaveBeenCalled();
    },
  );

  it("waits for Enter release before native dictation without submitting", async () => {
    const user = userEvent.setup();
    const { ref, insert } = editorRef();
    const toggle = vi.fn().mockResolvedValue({ ok: true, shortcut: "Ctrl+Alt+Shift+D" });
    const submit = vi.fn((event) => event.preventDefault());
    renderWithI18n(<form onSubmit={submit}>{nativeButton(ref, { toggle })}</form>);
    screen.getByRole("button", { name: "Dictate with Codex" }).focus();

    await user.keyboard("{Enter>}");
    expect(toggle).not.toHaveBeenCalled();
    await user.keyboard("{/Enter}");

    await waitFor(() => expect(toggle).toHaveBeenCalledExactlyOnceWith());
    expect(submit).not.toHaveBeenCalled();
    expect(insert).not.toHaveBeenCalled();
    expect(getUserMedia).not.toHaveBeenCalled();
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("handles IPC rejection without any paid fallback", async () => {
    const { ref } = editorRef();
    const toggle = vi.fn().mockRejectedValue(new Error("bridge disconnected"));
    renderWithI18n(nativeButton(ref, { toggle }));
    fireEvent.click(screen.getByRole("button", { name: "Dictate with Codex" }));
    await waitFor(() => expect(mocks.toastError).toHaveBeenCalledOnce());
    expect(getUserMedia).not.toHaveBeenCalled();
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("waits for a readonly-first composer to become visible and focused", async () => {
    vi.useFakeTimers();
    const { ref } = editorRef();
    vi.mocked(ref.current.focusForNativeInput).mockReturnValueOnce(false).mockReturnValueOnce(false);
    const toggle = vi.fn().mockResolvedValue({ ok: true, shortcut: "Ctrl+Alt+Shift+D" });
    const activate = vi.fn();
    renderWithI18n(nativeButton(ref, { toggle }, { onBeforeRecord: activate }));
    fireEvent.click(screen.getByRole("button", { name: "Dictate with Codex" }));
    expect(activate).toHaveBeenCalledOnce();
    expect(toggle).not.toHaveBeenCalled();
    await act(async () => { await vi.advanceTimersByTimeAsync(40); });
    expect(toggle).toHaveBeenCalledOnce();
    expect(getUserMedia).not.toHaveBeenCalled();
  });

  it("does not dispatch if the lazy editor never becomes ready", async () => {
    vi.useFakeTimers();
    const { ref } = editorRef();
    vi.mocked(ref.current.focusForNativeInput).mockReturnValue(false);
    const toggle = vi.fn();
    renderWithI18n(nativeButton(ref, { toggle }));
    fireEvent.click(screen.getByRole("button", { name: "Dictate with Codex" }));
    await act(async () => { await vi.advanceTimersByTimeAsync(1600); });
    expect(toggle).not.toHaveBeenCalled();
    expect(mocks.toastError).toHaveBeenCalledWith("The editor isn't ready yet. Try again.");
  });

  it("cancels preparation when the composer unmounts", async () => {
    vi.useFakeTimers();
    const ref = { current: null };
    const toggle = vi.fn();
    const view = renderWithI18n(nativeButton(ref, { toggle }));
    fireEvent.click(screen.getByRole("button", { name: "Dictate with Codex" }));
    view.unmount();
    await act(async () => { await vi.advanceTimersByTimeAsync(1600); });
    expect(toggle).not.toHaveBeenCalled();
    expect(mocks.toastError).not.toHaveBeenCalled();
  });

  it("cancels preparation when the composer becomes disabled", async () => {
    vi.useFakeTimers();
    const ref = { current: null };
    const toggle = vi.fn();
    const adapter = { toggle };
    const view = renderWithI18n(nativeButton(ref, adapter));
    fireEvent.click(screen.getByRole("button", { name: "Dictate with Codex" }));
    view.rerender(nativeButton(ref, adapter, { disabled: true }));
    await act(async () => { await vi.advanceTimersByTimeAsync(1600); });
    expect(toggle).not.toHaveBeenCalled();
    expect(mocks.toastError).not.toHaveBeenCalled();
  });

  it("deduplicates rapid clicks while the native request is pending", async () => {
    const { ref } = editorRef();
    let resolve!: (value: { ok: true; shortcut: string }) => void;
    const toggle = vi.fn(() => new Promise<{ ok: true; shortcut: string }>((complete) => { resolve = complete; }));
    renderWithI18n(nativeButton(ref, { toggle }));
    const button = screen.getByRole("button", { name: "Dictate with Codex" });
    fireEvent.click(button);
    fireEvent.click(button);
    expect(toggle).toHaveBeenCalledOnce();
    await act(async () => { resolve({ ok: true, shortcut: "Ctrl+Alt+Shift+D" }); });
  });

  it("localizes the native mic entry", () => {
    const { ref } = editorRef();
    renderWithI18n(nativeButton(ref, { toggle: vi.fn() }), { locale: "zh-Hans" });
    expect(screen.getByRole("button", { name: "使用 Codex 语音输入" })).toBeInTheDocument();
  });
});
