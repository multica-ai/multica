import { act, render } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { createRenderBudgetProbe } from "./testing/renderBudget";
import {
  useExternalStoreSelector,
  type ExternalStateStore
} from "./useExternalStoreSelector";

interface TestState {
  connection: string;
  counter: number;
}

interface TestStore extends ExternalStateStore<TestState> {
  listenerCount(): number;
  setState(next: TestState): void;
}

function createTestStore(initial: TestState): TestStore {
  const listeners = new Set<(state: TestState) => void>();
  const box = { state: initial };
  return {
    getSnapshot: () => box.state,
    listenerCount: () => listeners.size,
    setState(next) {
      box.state = next;
      for (const listener of listeners) {
        listener(next);
      }
    },
    subscribe(listener) {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    }
  };
}

function ConnectionLabel({ store }: { store: ExternalStateStore<TestState> }) {
  const connection = useExternalStoreSelector(
    store,
    (state) => state.connection
  );
  return <output>{connection}</output>;
}

describe("useExternalStoreSelector", () => {
  it("re-renders once when the selected slice changes", () => {
    const store = createTestStore({ connection: "unknown", counter: 0 });
    const probe = createRenderBudgetProbe();
    const { getByText } = render(
      probe.wrap("label", <ConnectionLabel store={store} />)
    );
    probe.reset();

    act(() => {
      store.setState({ connection: "connected", counter: 0 });
    });

    expect(getByText("connected")).toBeTruthy();
    expect(probe.renderCount("label")).toBe(1);
  });

  it("does not re-render when an unselected slice changes", () => {
    const store = createTestStore({ connection: "connected", counter: 0 });
    const probe = createRenderBudgetProbe();
    render(probe.wrap("label", <ConnectionLabel store={store} />));
    probe.reset();

    act(() => {
      store.setState({ connection: "connected", counter: 1 });
    });
    act(() => {
      store.setState({ connection: "connected", counter: 2 });
    });

    probe.assertRenderBudget({ label: 0 });
  });

  it("supports a custom equality function for derived selections", () => {
    const store = createTestStore({ connection: "connected", counter: 0 });
    const probe = createRenderBudgetProbe();

    function DerivedLabel() {
      // The selector returns a fresh object each run; the custom equality
      // keeps that from becoming a re-render (the trap the single binding
      // file exists to contain).
      const derived = useExternalStoreSelector(
        store,
        (state) => ({ label: state.connection }),
        (a, b) => a.label === b.label
      );
      return <output>{derived.label}</output>;
    }

    render(probe.wrap("derived", <DerivedLabel />));
    probe.reset();

    act(() => {
      store.setState({ connection: "connected", counter: 5 });
    });

    probe.assertRenderBudget({ derived: 0 });
  });

  it("unsubscribes from the engine on unmount", () => {
    const store = createTestStore({ connection: "connected", counter: 0 });
    const { unmount } = render(<ConnectionLabel store={store} />);
    expect(store.listenerCount()).toBe(1);
    unmount();
    expect(store.listenerCount()).toBe(0);
  });
});
