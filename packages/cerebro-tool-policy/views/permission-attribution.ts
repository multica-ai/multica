import {
  isLockedFromElsewhere,
  type ToolEffectiveSetting,
  type ToolLayer,
  type ToolPolicyRow,
  type ToolSetting,
} from "../core";

export const SETTING_LABEL: Record<ToolSetting, string> = {
  allow: "Allow",
  ask: "Ask",
  deny: "Deny",
  inherit: "Inherit",
  disable: "Disable",
};

// Restrictiveness rank mirrors the server contract and is shared by the
// row-level controls and the accepted-but-capped warning.
export const SETTING_RANK: Record<ToolEffectiveSetting, number> = {
  allow: 0,
  ask: 1,
  deny: 2,
};

export const LAYER_LABEL: Record<string, string> = {
  workspace: "Workspace",
  runtime: "Runtime",
  agent: "Agent",
  group: "Group",
  user: "User",
  system: "System",
  "": "Default",
};

export interface RowAttribution {
  kind: "override" | "inherited" | "capped";
  label: string;
  tooltip: string;
}

// originOf frames one row relative to the level this page authors: either the
// rule is an override set here, or it is inherited from the deciding layer.
export function originOf(
  row: ToolPolicyRow,
  editLayer: ToolLayer,
): { kind: "override" | "inherited"; level: string; label: string } {
  if (row.layers[editLayer]) {
    const level = LAYER_LABEL[editLayer] ?? editLayer;
    return { kind: "override", level, label: `Override on ${level}` };
  }
  const decided = row.effective.decided_by;
  const level = decided
    ? (LAYER_LABEL[decided] ?? decided)
    : "Workspace default";
  return { kind: "inherited", level, label: `Inherited from ${level}` };
}

function changeHint(layer: string): string {
  switch (layer) {
    case "workspace":
      return "Change it under Settings → Tools (workspace).";
    case "runtime":
      return "Change it under Runtime settings → Tools.";
    case "agent":
      return "Change it on this agent's Tools tab.";
    case "group":
      return "Change it under Settings → Groups.";
    case "user":
      return "It is set on the member's own permissions.";
    case "system":
      return "It is set on the autopilot's System rule (select the autopilot above).";
    default:
      return "";
  }
}

function formatGroupAttribution(row: ToolPolicyRow): string {
  return row.capped_by_groups
    .map((group) =>
      group.owner ? `${group.name} (owner: ${group.owner})` : group.name,
    )
    .join(", ");
}

function blockerText(row: ToolPolicyRow): { phrase: string; hint: string } {
  const blocker = row.effective.capped_by || row.effective.decided_by;
  if (blocker === "group" && row.capped_by_groups.length > 0) {
    return {
      phrase: `group ${formatGroupAttribution(row)}`,
      hint: changeHint("group"),
    };
  }
  return {
    phrase: LAYER_LABEL[blocker] ?? blocker,
    hint: changeHint(blocker),
  };
}

// Returns a user-facing explanation when an accepted write remains capped by a
// stricter layer. Tightening choices always take effect and return null.
export function futileWriteWarning(
  row: ToolPolicyRow,
  editLayer: ToolLayer,
  setting: ToolSetting,
): string | null {
  if (setting !== "allow" && setting !== "ask") return null;
  if (!isLockedFromElsewhere(row, editLayer)) return null;
  const effective = row.effective.setting;
  if (SETTING_RANK[setting] >= SETTING_RANK[effective]) return null;
  const { phrase, hint } = blockerText(row);
  return `"${SETTING_LABEL[setting]}" was saved on the ${LAYER_LABEL[editLayer]} layer, but it has no effect: the decision stays "${SETTING_LABEL[effective]}" because ${phrase} blocks it. ${hint}`.trim();
}

// rowAttribution is the single source of truth for the Origin badge and its
// navigation hint.
export function rowAttribution(
  row: ToolPolicyRow,
  editLayer: ToolLayer,
): RowAttribution {
  const locked = isLockedFromElsewhere(row, editLayer);
  const hasLocalOverride = !!row.layers[editLayer];
  const blocker = row.effective.capped_by || row.effective.decided_by;

  if (locked && hasLocalOverride && blocker !== editLayer) {
    const { phrase, hint } = blockerText(row);
    return {
      kind: "capped",
      label:
        blocker === "group"
          ? `Capped by group ${formatGroupAttribution(row)}`
          : `Capped by ${phrase}`,
      tooltip: `Your ${LAYER_LABEL[editLayer]} setting has no effect — blocked by ${phrase}. ${hint}`,
    };
  }

  const origin = originOf(row, editLayer);
  let tooltip =
    origin.kind === "override"
      ? `This rule is an override set on ${origin.level}.`
      : `No override on this level — the rule is inherited from ${origin.level}.`;
  if (locked) {
    const { phrase, hint } = blockerText(row);
    tooltip = `Blocked by ${phrase} — cannot be made more open here. ${hint}`;
  }
  return { kind: origin.kind, label: origin.label, tooltip };
}
