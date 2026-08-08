import { describe, expect, it, vi } from "vitest";

vi.mock("@multica/views/extensions", () => ({
  ExtensionsPage: () => null,
}));

import { appRoutes } from "./routes";

describe("desktop workspace routes", () => {
  it("registers the shared extensions page", () => {
    const workspaceRoute = appRoutes[0]?.children?.find(
      (route) => route.path === ":workspaceSlug",
    );
    const extensionsRoute = workspaceRoute?.children?.find(
      (route) => route.path === "extensions",
    );

    expect(extensionsRoute).toBeDefined();
    expect(extensionsRoute?.handle).toEqual({ title: "Extensions" });
  });
});
