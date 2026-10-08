import { StrictMode } from "react";
import { act, render, waitFor } from "@testing-library/react";
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
