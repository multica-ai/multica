import { beforeEach, describe, expect, it } from "vitest";
import {
  RUNTIME_COLUMN_KEYS,
  RUNTIME_DEFAULT_HIDDEN_COLUMNS,
  useRuntimesViewStore,
} from "./runtime-view-store";

describe("useRuntimesViewStore", () => {
  beforeEach(() => {
    useRuntimesViewStore.setState({
      hiddenColumns: [...RUNTIME_DEFAULT_HIDDEN_COLUMNS],
    });
  });

  it("hides only the opt-in Machine column by default", () => {
    // FIR-2669 follow-up: Machine (computer name) is opt-in so the default
    // runtime layout is unchanged; every other column stays visible.
    expect(RUNTIME_DEFAULT_HIDDEN_COLUMNS).toEqual(["machine"]);
    expect(useRuntimesViewStore.getState().hiddenColumns).toEqual(["machine"]);
  });

  it("offers Machine, Account and the existing secondary columns", () => {
    expect(RUNTIME_COLUMN_KEYS).toContain("machine");
    expect(RUNTIME_COLUMN_KEYS).toContain("account");
    expect(RUNTIME_COLUMN_KEYS).toContain("cli");
  });

  it("toggleColumn reveals the opt-in Machine column then hides it again", () => {
    const { toggleColumn } = useRuntimesViewStore.getState();
    toggleColumn("machine");
    expect(useRuntimesViewStore.getState().hiddenColumns).not.toContain(
      "machine",
    );
    toggleColumn("machine");
    expect(useRuntimesViewStore.getState().hiddenColumns).toContain("machine");
  });

  it("toggleColumn hides then restores a column", () => {
    const { toggleColumn } = useRuntimesViewStore.getState();
    toggleColumn("cost");
    expect(useRuntimesViewStore.getState().hiddenColumns).toContain("cost");
    toggleColumn("cost");
    expect(useRuntimesViewStore.getState().hiddenColumns).not.toContain("cost");
  });
});
