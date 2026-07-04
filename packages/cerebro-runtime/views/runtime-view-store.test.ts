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

  it("shows every column by default (picker only ever hides)", () => {
    expect(RUNTIME_DEFAULT_HIDDEN_COLUMNS).toEqual([]);
    expect(useRuntimesViewStore.getState().hiddenColumns).toEqual([]);
  });

  it("offers Account alongside the existing secondary columns", () => {
    expect(RUNTIME_COLUMN_KEYS).toContain("account");
    expect(RUNTIME_COLUMN_KEYS).toContain("cli");
  });

  it("toggleColumn hides then restores a column", () => {
    const { toggleColumn } = useRuntimesViewStore.getState();
    toggleColumn("cost");
    expect(useRuntimesViewStore.getState().hiddenColumns).toContain("cost");
    toggleColumn("cost");
    expect(useRuntimesViewStore.getState().hiddenColumns).not.toContain("cost");
  });
});
