import { describe, expect, it } from "vitest";
import { GET } from "./route";

describe("/favicon.ico", () => {
  it("redirects with a RELATIVE Location, whatever origin served the request", () => {
    // A standalone server behind a reverse proxy sees its own bind
    // address as the request origin. An absolute Location built from it
    // (http://0.0.0.0:3000/favicon.svg) is unreachable from any browser
    // and leaks the internal bind address — GH #7116. Relative resolves
    // against the origin the client actually used.
    const res = GET();

    expect(res.status).toBe(308);
    expect(res.headers.get("location")).toBe("/favicon.svg");
  });
});
