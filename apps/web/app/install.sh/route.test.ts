import { describe, expect, it } from "vitest";
import { GET } from "./route";

describe("GET /install.sh", () => {
  it("serves the Linux installer script directly", async () => {
    const response = await GET();
    const body = await response.text();

    expect(response.status).toBe(200);
    expect(response.headers.get("content-type")).toContain("text/x-shellscript");
    expect(body).toContain("#!/usr/bin/env bash");
    expect(body).toContain("Usage: install.sh");
  });
});
