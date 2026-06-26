import { describe, expect, it } from "vitest";
import { summarizeAccess } from "./access-summary";
import type { FolderGrant, GranteeType } from "./types";

function grant(p: Partial<FolderGrant>): FolderGrant {
  return {
    surface: p.surface ?? "artifact",
    folder_id: p.folder_id ?? "f1",
    grantee_type: p.grantee_type ?? "group",
    grantee_id: p.grantee_id ?? null,
    role: p.role ?? "viewer",
    source_folder_id: p.source_folder_id ?? "f1",
    is_direct: p.is_direct ?? true,
    depth: p.depth ?? 0,
  };
}

const labelFor = (type: GranteeType, id: string | null) =>
  id === "g-dk" ? "Country: s360 Denmark" : `${type} ${id ?? ""}`;

describe("summarizeAccess", () => {
  it("renders a workspace grant as Everyone", () => {
    const s = summarizeAccess(
      [grant({ grantee_type: "workspace", grantee_id: null })],
      labelFor,
    );
    expect(s).toEqual({ label: "Everyone", kind: "direct", tone: "everyone" });
  });

  it("renders a direct group grant by name", () => {
    const s = summarizeAccess(
      [grant({ grantee_type: "group", grantee_id: "g-dk" })],
      labelFor,
    );
    expect(s).toEqual({ label: "Country: s360 Denmark", kind: "direct", tone: "restricted" });
  });

  it("shows +N when several direct grants exist", () => {
    const s = summarizeAccess(
      [
        grant({ grantee_id: "g-dk" }),
        grant({ grantee_id: "g-2" }),
        grant({ grantee_id: "g-3" }),
      ],
      labelFor,
    );
    expect(s.label).toBe("Country: s360 Denmark +2");
    expect(s.kind).toBe("direct");
  });

  it("prefixes Inherits when only inherited grants exist", () => {
    const s = summarizeAccess(
      [grant({ grantee_id: "g-dk", is_direct: false, source_folder_id: "p" })],
      labelFor,
    );
    expect(s).toEqual({
      label: "Inherits: Country: s360 Denmark",
      kind: "inherited",
      tone: "inherited",
    });
  });

  it("prefers the direct grant over an inherited one", () => {
    const s = summarizeAccess(
      [
        grant({ grantee_id: "g-dk", is_direct: false, source_folder_id: "p" }),
        grant({ grantee_type: "workspace", grantee_id: null, is_direct: true }),
      ],
      labelFor,
    );
    expect(s).toEqual({ label: "Everyone", kind: "direct", tone: "everyone" });
  });

  it("falls back to Not set with no grants", () => {
    expect(summarizeAccess([], labelFor)).toEqual({
      label: "Not set",
      kind: "none",
      tone: "none",
    });
  });
});
