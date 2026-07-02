import { afterEach, describe, expect, it } from "vitest";
import { getDiagnosticRoute, setDiagnosticRoute } from "./diagnostic-route";

afterEach(() => setDiagnosticRoute(undefined));

describe("diagnostic route holder", () => {
  it("defaults to undefined", () => {
    expect(getDiagnosticRoute()).toBeUndefined();
  });

  it("stores and returns the last set template", () => {
    setDiagnosticRoute("/:slug/inbox");
    expect(getDiagnosticRoute()).toBe("/:slug/inbox");
    setDiagnosticRoute("/:slug/issues");
    expect(getDiagnosticRoute()).toBe("/:slug/issues");
  });

  it("normalizes empty string back to undefined", () => {
    setDiagnosticRoute("/:slug/inbox");
    setDiagnosticRoute("");
    expect(getDiagnosticRoute()).toBeUndefined();
  });
});
