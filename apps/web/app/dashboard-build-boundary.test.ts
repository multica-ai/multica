import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const layoutSource = readFileSync(
  resolve(__dirname, "[workspaceSlug]/(dashboard)/layout.tsx"),
  "utf8",
);

describe("dashboard build boundary", () => {
  it("keeps global overlay components out of the static dashboard layout graph", () => {
    expect(layoutSource).toContain('import dynamic from "next/dynamic"');
    expect(layoutSource).not.toMatch(/import\s+\{[^}]*SearchCommand[^}]*\}\s+from\s+["']@multica\/views\/search["']/);
    expect(layoutSource).not.toMatch(/import\s+\{[^}]*(ChatFab|ChatWindow)[^}]*\}\s+from\s+["']@multica\/views\/chat["']/);
    expect(layoutSource).toContain("@multica/views/search/command");
    expect(layoutSource).toContain("@multica/views/chat/fab");
    expect(layoutSource).toContain("@multica/views/chat/window");
  });
});
