import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(__dirname, "registry.tsx"), "utf8");

describe("ModalRegistry build boundary", () => {
  it("lazy-loads heavy modal bodies instead of statically importing all editor-backed modals", () => {
    expect(source).toContain("React.lazy");
    expect(source).not.toMatch(/import\s+\{\s*CreateIssueDialog\s*\}\s+from\s+["']\.\/create-issue-dialog["']/);
    expect(source).not.toMatch(/import\s+\{\s*CreateProjectModal\s*\}\s+from\s+["']\.\/create-project["']/);
    expect(source).not.toMatch(/import\s+\{\s*FeedbackModal\s*\}\s+from\s+["']\.\/feedback["']/);
    expect(source).toContain('import("./create-issue-dialog")');
    expect(source).toContain('import("./create-project")');
    expect(source).toContain('import("./feedback")');
  });
});
