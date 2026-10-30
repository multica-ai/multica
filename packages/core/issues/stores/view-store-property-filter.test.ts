// @vitest-environment node
import { describe, expect, it, beforeEach } from "vitest";
import { createStore, type StoreApi } from "zustand/vanilla";
import { viewStoreSlice, type IssueViewState } from "./view-store";

// The "__none__" sentinel is a plain string option id from the store's
// perspective; the filter menu (views) builds the array including it.
const NO_PROPERTY_VALUE = "__none__";
const PROP = "prop-estimate";

describe("setPropertyFilterValues", () => {
  let store: StoreApi<IssueViewState>;
  beforeEach(() => {
    store = createStore<IssueViewState>()((set) => viewStoreSlice(set));
  });

  it("replaces the property's full value set", () => {
    store.getState().togglePropertyFilter(PROP, "3.5");
    store.getState().setPropertyFilterValues(PROP, ["4"]);

    expect(store.getState().propertyFilters[PROP]).toEqual(["4"]);
  });

  it("clears the property filter when given an empty set", () => {
    store.getState().togglePropertyFilter(PROP, "3.5");
    store.getState().setPropertyFilterValues(PROP, []);

    expect(store.getState().propertyFilters[PROP]).toBeUndefined();
  });

  it("stores the no-value sentinel like any other value", () => {
    store.getState().setPropertyFilterValues(PROP, [NO_PROPERTY_VALUE]);

    expect(store.getState().propertyFilters[PROP]).toEqual([NO_PROPERTY_VALUE]);
  });
});
