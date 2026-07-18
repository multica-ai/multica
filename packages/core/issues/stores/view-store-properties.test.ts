import { createStore } from "zustand/vanilla";
import { describe, expect, it } from "vitest";

import {
  propertyIdFromViewKey,
  type IssueViewState,
  viewStorePersistOptions,
  viewStoreSlice,
} from "./view-store";

function createViewStore() {
  return createStore<IssueViewState>()(viewStoreSlice);
}

describe("custom properties in issue view state", () => {
  it("round-trips property view keys", () => {
    expect(propertyIdFromViewKey("property:business-value")).toBe("business-value");
    expect(propertyIdFromViewKey("priority")).toBeNull();
  });

  it("toggles property filters without leaving empty filter entries", () => {
    const store = createViewStore();

    store.getState().togglePropertyFilter("property-select", "option-a");
    store.getState().togglePropertyFilter("property-select", "option-b");
    expect(store.getState().propertyFilters).toEqual({
      "property-select": ["option-a", "option-b"],
    });

    store.getState().togglePropertyFilter("property-select", "option-a");
    store.getState().togglePropertyFilter("property-select", "option-b");
    expect(store.getState().propertyFilters).toEqual({});
  });

  it("clears property filters while preserving persisted custom card choices", () => {
    const store = createViewStore();
    store.getState().setGrouping("property:channel");
    store.getState().setSortBy("property:business-value");
    store.getState().togglePropertyFilter("property-channel", "webshop");
    store.getState().toggleCardPropertyId("property-business-value");
    store.getState().clearFilters();

    expect(store.getState().propertyFilters).toEqual({});
    expect(store.getState().cardPropertyIds).toEqual(["property-business-value"]);

    const persisted = viewStorePersistOptions("test").partialize(store.getState());
    expect(persisted).toMatchObject({
      grouping: "property:channel",
      sortBy: "property:business-value",
      propertyFilters: {},
      cardPropertyIds: ["property-business-value"],
    });
  });

  it("persists configurable Table columns, grouping, and hierarchy", () => {
    const store = createViewStore();

    store.getState().toggleTableColumn("priority");
    store.getState().toggleTableColumn("property:business-value");
    store.getState().reorderTableColumn("property:business-value", "status");
    store.getState().setTableColumnWidth("property:business-value", 240);
    store.getState().setTableGrouping("property:channel");
    store.getState().toggleTableHierarchy();

    const persisted = viewStorePersistOptions("test").partialize(store.getState());
    expect(persisted).toMatchObject({
      tableGrouping: "property:channel",
      tableHierarchy: false,
    });
    expect(persisted.tableColumns).toContainEqual({
      key: "property:business-value",
      width: 240,
    });
    expect(persisted.tableColumns.some((column) => column.key === "priority")).toBe(false);
  });
});
