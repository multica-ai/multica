// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import { buildIssueStatusCatalog } from "../../issue-statuses/queries";
import {
  combineFilterDependencies,
  filterDependencyState,
  statusCatalogSource,
  type FilterDependencySource,
} from "./filter-dependency";

function source(overrides: Partial<FilterDependencySource> = {}): FilterDependencySource {
  return { data: undefined, isError: false, refetch: vi.fn(), ...overrides };
}

const loaded = () => source({ data: [] });
const pending = () => source();
const failed = () => source({ isError: true });

describe("filterDependencyState", () => {
  it("is resolved when the filter is off, whatever its sources say", () => {
    expect(filterDependencyState(false, [pending(), failed()])).toMatchObject({
      pending: false,
      error: false,
      unresolved: false,
    });
  });

  it("waits while a source has no data", () => {
    expect(filterDependencyState(true, [loaded(), pending()])).toMatchObject({
      pending: true,
      error: false,
      unresolved: true,
    });
  });

  it("treats an empty answer as resolved, not as missing", () => {
    expect(filterDependencyState(true, [source({ data: [] })]).unresolved).toBe(false);
  });

  it("reports an error once any source fails, even while another is pending", () => {
    expect(filterDependencyState(true, [pending(), failed()])).toMatchObject({
      pending: false,
      error: true,
      unresolved: true,
    });
  });

  it("keeps answering from data kept behind a failed refetch", () => {
    expect(filterDependencyState(true, [source({ data: ["a"], isError: true })]).unresolved).toBe(false);
  });

  it("ignores sources the active filter does not read", () => {
    expect(filterDependencyState(true, [false, loaded()]).unresolved).toBe(false);
    expect(filterDependencyState(true, [false, pending()]).pending).toBe(true);
  });

  it("retries only the sources that failed", () => {
    const ok = loaded();
    const waiting = pending();
    const broken = failed();
    filterDependencyState(true, [ok, waiting, broken]).retry();
    expect(broken.refetch).toHaveBeenCalledTimes(1);
    expect(waiting.refetch).not.toHaveBeenCalled();
    expect(ok.refetch).not.toHaveBeenCalled();
  });
});

describe("combineFilterDependencies", () => {
  it("waits while any part is pending, and fails once any part failed", () => {
    const waiting = filterDependencyState(true, [pending()]);
    const resolved = filterDependencyState(true, [loaded()]);
    expect(combineFilterDependencies(resolved, waiting)).toMatchObject({ pending: true, error: false });
    expect(combineFilterDependencies(resolved, resolved).unresolved).toBe(false);

    const broken = failed();
    const combined = combineFilterDependencies(waiting, filterDependencyState(true, [broken]));
    expect(combined).toMatchObject({ pending: false, error: true, unresolved: true });
    combined.retry();
    expect(broken.refetch).toHaveBeenCalledTimes(1);
  });
});

describe("statusCatalogSource", () => {
  it("has data only once the catalog loaded, and retries through the catalog", () => {
    const retry = vi.fn();
    expect(statusCatalogSource(buildIssueStatusCatalog(undefined, { isPending: true })).data).toBeUndefined();
    expect(statusCatalogSource(buildIssueStatusCatalog([])).data).toEqual([]);
    const failedCatalog = statusCatalogSource(buildIssueStatusCatalog(undefined, { isError: true, retry }));
    expect(failedCatalog.isError).toBe(true);
    filterDependencyState(true, [failedCatalog]).retry();
    expect(retry).toHaveBeenCalledTimes(1);
  });
});
