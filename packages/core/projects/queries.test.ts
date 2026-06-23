import { describe, expect, it } from "vitest";
import { projectAictxStatusOptions, projectKeys } from "./queries";

describe("project AICTX status query options", () => {
  it("uses a workspace-scoped project detail query key", () => {
    expect(projectKeys.aictxStatus("ws-1", "project-1")).toEqual([
      "projects",
      "ws-1",
      "detail",
      "project-1",
      "aictx",
      "status",
    ]);
  });

  it("fetches the project AICTX status through the API client", async () => {
    const options = projectAictxStatusOptions("ws-1", "project-1");

    expect(options.queryKey).toEqual(projectKeys.aictxStatus("ws-1", "project-1"));
  });
});
