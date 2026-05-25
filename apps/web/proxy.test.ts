import { describe, expect, it } from "vitest";
import { NextRequest } from "next/server";
import { proxy } from "./proxy";

function makeRequest(pathname: string, cookie = "") {
  return new NextRequest(`http://localhost:3000${pathname}`, {
    headers: cookie ? { cookie } : undefined,
  });
}

describe("proxy", () => {
  it("redirects logged-in root requests to the blank workspace home", () => {
    const res = proxy(
      makeRequest(
        "/",
        "multica_logged_in=1; last_workspace_slug=openharness",
      ),
    );

    expect(res.status).toBe(307);
    expect(res.headers.get("location")).toBe("http://localhost:3000/openharness");
  });

  it("keeps legacy issue links under the last workspace", () => {
    const res = proxy(
      makeRequest(
        "/issues/OPE-1?comment=c1",
        "multica_logged_in=1; last_workspace_slug=openharness",
      ),
    );

    expect(res.status).toBe(307);
    expect(res.headers.get("location")).toBe(
      "http://localhost:3000/openharness/issues/OPE-1?comment=c1",
    );
  });
});
