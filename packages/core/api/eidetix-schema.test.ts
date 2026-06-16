import { describe, expect, it } from "vitest";
import { parseWithFallback } from "./schema";
import { EidetixConfigSchema, EMPTY_EIDETIX_CONFIG } from "./schemas";

const opts = { endpoint: "GET /api/projects/{id}/eidetix" };

describe("EidetixConfigSchema", () => {
  it("parses a well-formed response", () => {
    const got = parseWithFallback(
      { configured: true, enabled: true, endpoint_url: "https://e/sse", graph_label: "Marketing" },
      EidetixConfigSchema, EMPTY_EIDETIX_CONFIG, opts,
    );
    expect(got).toEqual({ configured: true, enabled: true, endpoint_url: "https://e/sse", graph_label: "Marketing" });
  });
  it("defaults missing optional fields", () => {
    const got = parseWithFallback({ configured: true, enabled: false }, EidetixConfigSchema, EMPTY_EIDETIX_CONFIG, opts);
    expect(got.endpoint_url).toBe("");
    expect(got.graph_label).toBe("");
  });
  it("falls back when a required field is the wrong type", () => {
    const got = parseWithFallback({ configured: "yes", enabled: true }, EidetixConfigSchema, EMPTY_EIDETIX_CONFIG, opts);
    expect(got).toBe(EMPTY_EIDETIX_CONFIG);
  });
  it("falls back on null", () => {
    expect(parseWithFallback(null, EidetixConfigSchema, EMPTY_EIDETIX_CONFIG, opts)).toBe(EMPTY_EIDETIX_CONFIG);
  });
});
