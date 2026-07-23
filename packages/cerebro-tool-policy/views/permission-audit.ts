export interface PermissionAuditMember {
  id: string;
  name: string;
  email: string;
  role: string;
}

export interface PermissionAuditAgent {
  id: string;
  name: string;
  ownerId: string | null;
  runtimeId: string;
}

export interface PermissionAuditRuntime {
  id: string;
  name: string;
  ownerId: string | null;
}

export interface PermissionAuditGroup {
  id: string;
  name: string;
  userIds: string[];
}

export interface PermissionAuditHolder {
  layer: string;
  subject_id: string;
  setting: string;
}

export interface PermissionAuditContext {
  id: string;
  userId: string;
  agentId?: string;
  runtimeId?: string;
  label: string;
}

export interface PermissionAuditResolved {
  setting: string;
  decidedBy: string;
  cappedBy: string;
  policySetting?: string;
  availabilityLevel?: string;
  governanceSeverity?: PermissionAuditSeverity;
}

export interface PermissionAuditCell {
  id: string;
  label: string;
  setting: string | null;
}

export interface PermissionAuditEffectiveCell {
  contextLabel: string;
  setting: string;
  source: string;
  policySetting?: string;
  availabilityLevel?: string;
  governanceSeverity?: PermissionAuditSeverity;
}

export type PermissionAuditSeverity = "critical" | "high" | "medium" | "low";

export interface PermissionAuditRow {
  user: PermissionAuditMember;
  severity: PermissionAuditSeverity;
  workspace: PermissionAuditCell[];
  runtimes: PermissionAuditCell[];
  agents: PermissionAuditCell[];
  groups: PermissionAuditCell[];
  direct: PermissionAuditCell[];
  effective: PermissionAuditEffectiveCell[];
}

const SEVERITY_RANK: Record<PermissionAuditSeverity, number> = {
  critical: 0,
  high: 1,
  medium: 2,
  low: 3,
};

function auditSeverity(
  effective: PermissionAuditEffectiveCell[],
): PermissionAuditSeverity {
  const severityFor = (
    cell: PermissionAuditEffectiveCell,
  ): PermissionAuditSeverity => {
    if (cell.governanceSeverity) return cell.governanceSeverity;
    if (
      cell.policySetting === "allow" &&
      cell.availabilityLevel !== undefined &&
      cell.availabilityLevel !== "verified"
    ) {
      return "critical";
    }
    if (cell.setting === "allow") return "high";
    if (cell.setting === "ask") return "medium";
    return "low";
  };

  return effective.reduce<PermissionAuditSeverity>((worst, cell) => {
    const severity = severityFor(cell);
    return SEVERITY_RANK[severity] < SEVERITY_RANK[worst] ? severity : worst;
  }, "low");
}

function byName<T extends { name: string }>(a: T, b: T): number {
  return a.name.localeCompare(b.name);
}

export function buildPermissionAuditContexts({
  members,
  agents,
  runtimes,
}: {
  members: PermissionAuditMember[];
  agents: PermissionAuditAgent[];
  runtimes: PermissionAuditRuntime[];
}): PermissionAuditContext[] {
  const contexts: PermissionAuditContext[] = [];

  for (const member of [...members].sort(byName)) {
    const ownedAgents = agents
      .filter((agent) => agent.ownerId === member.id)
      .sort(byName);
    const usedRuntimeIds = new Set<string>();

    for (const agent of ownedAgents) {
      if (agent.runtimeId) usedRuntimeIds.add(agent.runtimeId);
      contexts.push({
        id: `${member.id}:${agent.id}:${agent.runtimeId}`,
        userId: member.id,
        agentId: agent.id,
        ...(agent.runtimeId ? { runtimeId: agent.runtimeId } : {}),
        label: agent.name,
      });
    }

    const unusedOwnedRuntimes = runtimes
      .filter(
        (runtime) =>
          runtime.ownerId === member.id && !usedRuntimeIds.has(runtime.id),
      )
      .sort(byName);
    for (const runtime of unusedOwnedRuntimes) {
      contexts.push({
        id: `${member.id}::${runtime.id}`,
        userId: member.id,
        runtimeId: runtime.id,
        label: runtime.name,
      });
    }

    if (ownedAgents.length === 0 && unusedOwnedRuntimes.length === 0) {
      contexts.push({
        id: `${member.id}::`,
        userId: member.id,
        label: "User only",
      });
    }
  }

  return contexts;
}

function layerName(layer: string): string {
  const labels: Record<string, string> = {
    workspace: "Workspace",
    runtime: "Runtime",
    agent: "Agent",
    group: "Group",
    user: "Direct",
    system: "System",
  };
  return (
    labels[layer] ??
    (layer ? `${layer[0]!.toUpperCase()}${layer.slice(1)}` : "Default")
  );
}

export function buildPermissionAuditRows({
  members,
  agents,
  runtimes,
  groups,
  holders,
  contexts,
  resolvedByContext,
}: {
  members: PermissionAuditMember[];
  agents: PermissionAuditAgent[];
  runtimes: PermissionAuditRuntime[];
  groups: PermissionAuditGroup[];
  holders: PermissionAuditHolder[];
  contexts: PermissionAuditContext[];
  resolvedByContext: ReadonlyMap<string, PermissionAuditResolved>;
}): PermissionAuditRow[] {
  const settingFor = (layer: string, subjectId: string): string | null =>
    holders.find(
      (holder) => holder.layer === layer && holder.subject_id === subjectId,
    )?.setting ?? null;
  const workspaceHolder = holders.find((holder) => holder.layer === "workspace");
  const runtimeById = new Map(runtimes.map((runtime) => [runtime.id, runtime]));

  const rows = [...members].sort(byName).map((member) => {
    const memberContexts = contexts.filter(
      (context) => context.userId === member.id,
    );
    const memberAgents = agents
      .filter((agent) => agent.ownerId === member.id)
      .sort(byName);
    const runtimeIds = new Set(
      memberContexts
        .map((context) => context.runtimeId)
        .filter((id): id is string => Boolean(id)),
    );
    const memberRuntimes = [...runtimeIds]
      .map((id) => runtimeById.get(id))
      .filter((runtime): runtime is PermissionAuditRuntime => Boolean(runtime))
      .sort(byName);
    const memberGroups = groups
      .filter((group) => group.userIds.includes(member.id))
      .sort(byName);

    const effective = memberContexts.flatMap((context) => {
      const resolved = resolvedByContext.get(context.id);
      if (!resolved) return [];
      return [
        {
          contextLabel: context.label,
          setting: resolved.setting,
          source: layerName(resolved.cappedBy || resolved.decidedBy),
          policySetting: resolved.policySetting,
          availabilityLevel: resolved.availabilityLevel,
          governanceSeverity: resolved.governanceSeverity,
        },
      ];
    });

    return {
      user: member,
      severity: auditSeverity(effective),
      workspace: [
        {
          id: workspaceHolder?.subject_id ?? "workspace",
          label: "Whole workspace",
          setting: workspaceHolder?.setting ?? null,
        },
      ],
      runtimes: memberRuntimes.map((runtime) => ({
        id: runtime.id,
        label: runtime.name,
        setting: settingFor("runtime", runtime.id),
      })),
      agents: memberAgents.map((agent) => ({
        id: agent.id,
        label: agent.name,
        setting: settingFor("agent", agent.id),
      })),
      groups: memberGroups.map((group) => ({
        id: group.id,
        label: group.name,
        setting: settingFor("group", group.id),
      })),
      direct: [
        {
          id: member.id,
          label: member.name,
          setting: settingFor("user", member.id),
        },
      ],
      effective,
    };
  });

  return rows.sort((a, b) => {
    const severity = SEVERITY_RANK[a.severity] - SEVERITY_RANK[b.severity];
    return severity || byName(a.user, b.user);
  });
}
