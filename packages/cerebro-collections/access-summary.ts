// Turns a folder's effective grants into the one-line access summary shown on
// the folder card (the pill in Jesper's mockup: "Everyone", "Country: s360
// Denmark", "Inherits: Country: s360 Denmark"). Pure so it can be unit-tested
// without React or the network.

import type { FolderGrant, GranteeType } from "./types";

export type AccessKind = "direct" | "inherited" | "none";

// Drives the colored dot on the pill, mirroring the mockup: a green dot for a
// workspace-wide grant ("Everyone"), blue for a restricted (group/member/…)
// grant, muted for inherited, grey for unset.
export type AccessTone = "everyone" | "restricted" | "inherited" | "none";

export interface AccessSummary {
  label: string;
  kind: AccessKind;
  tone: AccessTone;
}

// A grant on the whole workspace reads as "Everyone"; everything else resolves
// through the caller-supplied directory (group / member / agent / runtime name).
function granteeLabel(
  g: Pick<FolderGrant, "grantee_type" | "grantee_id">,
  labelFor: (type: GranteeType, id: string | null) => string,
): string {
  if (g.grantee_type === "workspace") return "Everyone";
  return labelFor(g.grantee_type, g.grantee_id);
}

// `grants` is the EFFECTIVE view for one folder: direct grants (is_direct) set
// on the folder itself plus inherited grants cascaded from an ancestor. A
// folder's own access wins the pill; if it only inherits, the pill says so;
// with neither, access is whatever the workspace default is ("Not set" here).
export function summarizeAccess(
  grants: FolderGrant[],
  labelFor: (type: GranteeType, id: string | null) => string,
): AccessSummary {
  const direct = grants.filter((g) => g.is_direct);
  if (direct.length > 0) {
    const primary = direct[0]!;
    const extra = direct.length > 1 ? ` +${direct.length - 1}` : "";
    return {
      label: `${granteeLabel(primary, labelFor)}${extra}`,
      kind: "direct",
      tone: primary.grantee_type === "workspace" ? "everyone" : "restricted",
    };
  }
  const inherited = grants.filter((g) => !g.is_direct);
  if (inherited.length > 0) {
    return {
      label: `Inherits: ${granteeLabel(inherited[0]!, labelFor)}`,
      kind: "inherited",
      tone: "inherited",
    };
  }
  return { label: "Not set", kind: "none", tone: "none" };
}
