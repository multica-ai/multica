import { StrictMode } from "react";
import { act, render, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useAutoSave } from "./use-auto-save";

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

function AutoSaveHarness({
  value,
  onSave,
  onSuccess,
}: {
  value: string;
  onSave: (value: string) => Promise<void>;
  onSuccess: (value: string) => void;
}) {
  useAutoSave({
    value,
    savedValue: "",
    onSave,
    onSuccess,
    delay: 650,
    isEqual: (left, right) => left === right,
  });
  return null;
}

describe("useAutoSave success feedback", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("reports success after the persisted value resolves", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const onSave = vi.fn(async () => undefined);
    const onSuccess = vi.fn();
    render(
      <AutoSaveHarness value="saved" onSave={onSave} onSuccess={onSuccess} />,
    );

    act(() => vi.advanceTimersByTime(650));

    await waitFor(() => {
      expect(onSave).toHaveBeenCalledWith("saved");
      expect(onSuccess).toHaveBeenCalledWith("saved");
    });
  });

  it("still reports success under StrictMode's double-invoked mount", async () => {
    // StrictMode runs setup/cleanup/setup on mount. A mount effect that only
    // clears the mounted flag in cleanup leaves it false for the component's
    // life, so a successful save is silently dropped: neither the "saved"
    // status nor onSuccess ever fires and the indicator spins forever.
    const onSave = vi.fn(async () => undefined);
    const onSuccess = vi.fn();
    render(
      <StrictMode>
        <AutoSaveHarness value="saved" onSave={onSave} onSuccess={onSuccess} />
      </StrictMode>,
    );

    await waitFor(() => expect(onSave).toHaveBeenCalledWith("saved"));
    await waitFor(() => expect(onSuccess).toHaveBeenCalledWith("saved"));
  });

  it("waits for the latest queued value before reporting success", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const first = deferred<void>();
    const second = deferred<void>();
    const onSave = vi
      .fn<(value: string) => Promise<void>>()
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise);
    const onSuccess = vi.fn();
    const { rerender } = render(
      <AutoSaveHarness value="first" onSave={onSave} onSuccess={onSuccess} />,
    );

    act(() => vi.advanceTimersByTime(650));
    await waitFor(() => expect(onSave).toHaveBeenCalledWith("first"));

    rerender(
      <AutoSaveHarness value="second" onSave={onSave} onSuccess={onSuccess} />,
    );
    act(() => vi.advanceTimersByTime(650));

    await act(async () => first.resolve());
    await waitFor(() => expect(onSave).toHaveBeenCalledWith("second"));
    expect(onSuccess).not.toHaveBeenCalled();

    await act(async () => second.resolve());
    await waitFor(() => expect(onSuccess).toHaveBeenCalledWith("second"));
    expect(onSuccess).toHaveBeenCalledTimes(1);
  });
});

const equalStrings = (left: string, right: string) => left === right;

describe("useAutoSave reverted drafts", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  function setup() {
    vi.useFakeTimers();
    const first = deferred<void>();
    const second = deferred<void>();
    const onSave = vi
      .fn<(value: string) => Promise<void>>()
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise);
    const onSuccess = vi.fn();
    const onError = vi.fn();
    const hook = renderHook(
      ({ value, enabled, savedValue }) =>
        useAutoSave({
          value,
          savedValue,
          enabled,
          onSave,
          onSuccess,
          onError,
          isEqual: equalStrings,
        }),
      { initialProps: { value: "A", enabled: true, savedValue: "A" } },
    );
    const edit = (value: string) =>
      hook.rerender({ value, enabled: true, savedValue: "A" });
    edit("B");
    act(() => hook.result.current.flush());
    expect(onSave).toHaveBeenCalledExactlyOnceWith("B");
    return { ...hook, edit, first, second, onSave, onSuccess, onError };
  }

  it.each(["blur", "debounce"])(
    "persists a revert to the original value through %s",
    async (trigger) => {
      const hook = setup();
      hook.edit("A");
      act(() => {
        if (trigger === "blur") hook.result.current.flush();
        else vi.advanceTimersByTime(650);
      });
      expect(hook.onSave).toHaveBeenCalledTimes(1);
      await act(async () => hook.first.resolve());
      expect(hook.onSave.mock.calls).toEqual([["B"], ["A"]]);
      expect(hook.result.current.status).toBe("saving");
      expect(hook.onSuccess).not.toHaveBeenCalled();
      await act(async () => hook.second.resolve());
      expect(hook.result.current.status).toBe("saved");
      expect(hook.onSuccess).toHaveBeenCalledExactlyOnceWith("A");
    },
  );

  it("replaces an already queued draft with the latest revert", async () => {
    const hook = setup();
    hook.edit("C");
    act(() => hook.result.current.flush());
    hook.edit("A");
    await act(async () => hook.first.resolve());
    expect(hook.onSave.mock.calls).toEqual([["B"], ["A"]]);
    await act(async () => hook.second.resolve());
    expect(hook.onSuccess).toHaveBeenCalledExactlyOnceWith("A");
  });

  it("keeps success pending until a still-debouncing revert is saved", async () => {
    const hook = setup();
    hook.edit("A");
    act(() => vi.advanceTimersByTime(649));
    await act(async () => hook.first.resolve());
    expect(hook.onSave).toHaveBeenCalledTimes(1);
    expect(hook.onSuccess).not.toHaveBeenCalled();
    expect(hook.result.current.status).toBe("saving");
    act(() => vi.advanceTimersByTime(1));
    expect(hook.onSave.mock.calls).toEqual([["B"], ["A"]]);
    await act(async () => hook.second.resolve());
    expect(hook.onSuccess).toHaveBeenCalledExactlyOnceWith("A");
  });

  it("avoids a redundant correction when the earlier request fails", async () => {
    const hook = setup();
    hook.edit("A");
    act(() => hook.result.current.flush());
    const error = new Error("Save rejected");
    await act(async () => hook.first.reject(error));
    expect(hook.onSave).toHaveBeenCalledTimes(1);
    expect(hook.onSuccess).not.toHaveBeenCalled();
    expect(hook.onError).toHaveBeenCalledExactlyOnceWith(error);
    expect(hook.result.current.status).toBe("error");
  });

  it("honors permission revocation before draining a queued revert", async () => {
    const hook = setup();
    hook.edit("A");
    act(() => hook.result.current.flush());
    hook.rerender({ value: "A", enabled: false, savedValue: "A" });
    await act(async () => hook.first.resolve());
    expect(hook.onSave).toHaveBeenCalledTimes(1);
    expect(hook.onSuccess).not.toHaveBeenCalled();
    expect(hook.result.current.status).toBe("idle");
  });

  it("cancels pending debounce and success feedback on unmount", async () => {
    const hook = setup();
    hook.edit("A");
    hook.unmount();
    await act(async () => hook.first.resolve());
    act(() => vi.advanceTimersByTime(650));
    expect(hook.onSave).toHaveBeenCalledTimes(1);
    expect(hook.onSuccess).not.toHaveBeenCalled();
  });

  it("persists a draft queued by blur after unmount without success feedback", async () => {
    const hook = setup();
    hook.edit("BC");
    act(() => hook.result.current.flush());
    expect(hook.onSave).toHaveBeenCalledExactlyOnceWith("B");

    hook.unmount();
    await act(async () => hook.first.resolve());
    expect(hook.onSave.mock.calls).toEqual([["B"], ["BC"]]);

    await act(async () => hook.second.resolve());
    expect(hook.onSuccess).not.toHaveBeenCalled();
  });

  it("persists an empty reverted draft after the in-flight write", async () => {
    vi.useFakeTimers();
    const first = deferred<void>();
    const second = deferred<void>();
    const onSave = vi
      .fn<(value: string) => Promise<void>>()
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise);
    const hook = renderHook(
      ({ value }) =>
        useAutoSave({
          value,
          savedValue: "",
          onSave,
          isEqual: equalStrings,
        }),
      { initialProps: { value: "" } },
    );
    hook.rerender({ value: "B" });
    act(() => hook.result.current.flush());
    hook.rerender({ value: "" });
    act(() => hook.result.current.flush());
    await act(async () => first.resolve());
    expect(onSave.mock.calls).toEqual([["B"], [""]]);
    await act(async () => second.resolve());
    expect(hook.result.current.status).toBe("saved");
  });

  it("keeps an unchanged confirmed value idle without issuing requests", () => {
    vi.useFakeTimers();
    const onSave = vi.fn(async () => undefined);
    const hook = renderHook(() =>
      useAutoSave({
        value: "A",
        savedValue: "A",
        onSave,
        isEqual: equalStrings,
      }),
    );
    act(() => {
      hook.result.current.flush();
      vi.advanceTimersByTime(650);
    });
    expect(onSave).not.toHaveBeenCalled();
    expect(hook.result.current.status).toBe("idle");
  });

  it("accepts externally confirmed saved values without writing them back", () => {
    vi.useFakeTimers();
    const onSave = vi.fn(async () => undefined);
    const hook = renderHook(
      ({ value }) =>
        useAutoSave({
          value,
          savedValue: value,
          onSave,
          isEqual: equalStrings,
        }),
      { initialProps: { value: "A" } },
    );
    hook.rerender({ value: "B" });
    act(() => vi.advanceTimersByTime(650));
    expect(onSave).not.toHaveBeenCalled();
  });
});
