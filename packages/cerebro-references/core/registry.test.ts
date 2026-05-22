import { beforeEach, describe, expect, it } from "vitest";
import {
  __resetObjectRenderers,
  fallbackObjectRenderer,
  getObjectRenderer,
  listRegisteredObjects,
  registerObjectRenderer,
} from "./registry";
import type { IssueReference } from "./types";

function makeRef(overrides: Partial<IssueReference> = {}): IssueReference {
  return {
    id: "ref-1",
    issue_id: "issue-1",
    object: "github_pr",
    type: null,
    ref_id: "firtal-group/firtal-cerebro#42",
    label: null,
    url: null,
    metadata: {},
    created_by_type: "member",
    created_by_id: "user-1",
    created_at: "2026-05-19T00:00:00Z",
    updated_at: "2026-05-19T00:00:00Z",
    ...overrides,
  };
}

describe("registry", () => {
  beforeEach(() => {
    __resetObjectRenderers();
  });

  it("registers and retrieves a renderer", () => {
    registerObjectRenderer({
      object: "stripe_customer",
      formatTitle: (ref) => `Stripe ${ref.ref_id}`,
    });
    const r = getObjectRenderer("stripe_customer");
    expect(r.object).toBe("stripe_customer");
    expect(r.formatTitle(makeRef({ object: "stripe_customer", ref_id: "cus_123" }))).toBe(
      "Stripe cus_123",
    );
  });

  it("layers fallback methods onto partially-populated renderers", () => {
    registerObjectRenderer({
      object: "stripe_customer",
      formatTitle: () => "ignored",
    });
    const r = getObjectRenderer("stripe_customer");
    expect(r.formatBadge(makeRef())).toBe(fallbackObjectRenderer.formatBadge(makeRef()));
    expect(r.resolveUrl(makeRef({ url: "https://stripe" }))).toBe("https://stripe");
  });

  it("returns the fallback renderer for an unknown object", () => {
    const ref = makeRef({ object: "linear_issue", label: "Linear LIN-1" });
    const r = getObjectRenderer("linear_issue");
    expect(r.object).toBe("linear_issue");
    expect(r.formatTitle(ref)).toBe("Linear LIN-1");
    expect(r.formatBadge(ref)).toBeNull();
    expect(r.resolveUrl(ref)).toBeNull();
  });

  it("fallback formatTitle uses ref_id when label is missing", () => {
    const ref = makeRef({ object: "unknown_kind", label: null, ref_id: "raw-id" });
    expect(getObjectRenderer("unknown_kind").formatTitle(ref)).toBe("raw-id");
  });

  it("lists every registered object", () => {
    registerObjectRenderer({ object: "github_pr" });
    registerObjectRenderer({ object: "stripe_customer" });
    expect(listRegisteredObjects().sort()).toEqual(["github_pr", "stripe_customer"]);
  });
});
