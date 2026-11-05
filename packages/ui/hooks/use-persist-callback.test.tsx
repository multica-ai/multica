// @vitest-environment jsdom
import { fireEvent, render, renderHook } from "@testing-library/react";
import { memo, StrictMode, useMemo, type PropsWithChildren } from "react";
import { describe, expect, it, vi } from "vitest";
import { usePersistCallback } from "./use-persist-callback";

const Strict = ({ children }: PropsWithChildren) => (
  <StrictMode>{children}</StrictMode>
);

describe("usePersistCallback", () => {
  it("keeps its identity while using the latest render's props and callback", () => {
    const { result, rerender } = renderHook(
      ({ value }) => usePersistCallback((suffix: string) => value + suffix),
      {
        initialProps: { value: "first" },
        wrapper: Strict
      }
    );
    const callback = result.current;
    expect(callback("!")).toBe("first!");
    rerender({ value: "second" });
    expect(result.current).toBe(callback);
    expect(callback("!")).toBe("second!");
  });

  it("preserves arguments, return values, this and asynchronous results", async () => {
    const { result } = renderHook(() =>
      usePersistCallback(async function (
        this: { offset: number },
        input: number
      ) {
        return this.offset + input;
      })
    );
    await expect(result.current.call({ offset: 3 }, 4)).resolves.toBe(7);
  });

  it("allows a render slot to read the current render before effects run", () => {
    const { result, rerender } = renderHook(
      ({ label }) => {
        const render = usePersistCallback(() => label);
        return render();
      },
      { initialProps: { label: "first" } }
    );
    rerender({ label: "second" });
    expect(result.current).toBe("second");
  });
  it("updates an event without rerendering a child whose props stay stable", () => {
    let renders = 0;
    const selected = vi.fn<(value: number) => void>();
    const Child = memo(function Child({
      data,
      onClick
    }: {
      data: { label: string };
      onClick(): void;
    }) {
      renders++;
      return <button onClick={onClick}>{data.label}</button>;
    });
    function Parent({ value }: { value: number }) {
      const data = useMemo(() => ({ label: "Select" }), []);
      const onClick = usePersistCallback(() => selected(value));
      return <Child data={data} onClick={onClick} />;
    }
    const view = render(<Parent value={1} />);
    view.rerender(<Parent value={2} />);
    expect(renders).toBe(1);
    fireEvent.click(view.getByRole("button", { name: "Select" }));
    expect(selected).toHaveBeenCalledWith(2);
  });
});
