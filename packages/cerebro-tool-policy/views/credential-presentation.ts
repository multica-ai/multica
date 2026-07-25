import type { ToolPolicyRow } from "../core";

const CREDENTIAL_CAP_ORDER = [
  "credential.reveal",
  "credential.read_redacted",
  "credential.rotate",
  "credential.revoke",
  "credential.attach",
];

export interface CredentialGroupData {
  resource: string;
  label: string;
  rows: ToolPolicyRow[];
}

export function groupCredentialRows(
  rows: ToolPolicyRow[],
  search: string,
): CredentialGroupData[] {
  const q = search.trim().toLowerCase();
  const byResource = new Map<string, ToolPolicyRow[]>();
  for (const row of rows) {
    const group = byResource.get(row.resource_pattern);
    if (group) group.push(row);
    else byResource.set(row.resource_pattern, [row]);
  }
  const rank = (key: string) => {
    const index = CREDENTIAL_CAP_ORDER.indexOf(key);
    return index === -1 ? CREDENTIAL_CAP_ORDER.length : index;
  };
  const labelFor = (resource: string, group: ToolPolicyRow[]) =>
    group[0]?.category?.trim() ||
    resource.replace(/^[^:]*:/, "") ||
    resource;

  return [...byResource.entries()]
    .map(([resource, group]) => ({
      resource,
      label: labelFor(resource, group),
      rows: [...group].sort(
        (left, right) => rank(left.tool_key) - rank(right.tool_key),
      ),
    }))
    .filter((group) => !q || group.label.toLowerCase().includes(q))
    .sort((left, right) => left.label.localeCompare(right.label));
}

const CREDENTIAL_TRACKS = ["agents", "members", "shared"];

export interface CredentialTreeNode {
  key: string;
  label: string;
  children: CredentialTreeNode[];
  group?: CredentialGroupData;
}

export function buildCredentialTree(
  groups: CredentialGroupData[],
): CredentialTreeNode[] {
  const roots: CredentialTreeNode[] = [];
  const findOrAdd = (
    siblings: CredentialTreeNode[],
    key: string,
    label: string,
  ): CredentialTreeNode => {
    let node = siblings.find((sibling) => sibling.key === key);
    if (!node) {
      node = { key, label, children: [] };
      siblings.push(node);
    }
    return node;
  };

  for (const group of groups) {
    const name = group.label;
    const firstDash = name.indexOf("-");
    const track = firstDash === -1 ? "" : name.slice(0, firstDash);
    if (firstDash === -1 || !CREDENTIAL_TRACKS.includes(track)) {
      roots.push({
        key: group.resource,
        label: name,
        children: [],
        group,
      });
      continue;
    }
    const rest = name.slice(firstDash + 1);
    const secondDash = rest.indexOf("-");
    const owner = secondDash === -1 ? rest : rest.slice(0, secondDash);
    const leafLabel = secondDash === -1 ? rest : rest.slice(secondDash + 1);
    const trackNode = findOrAdd(roots, `track:${track}`, track);
    const ownerNode = findOrAdd(
      trackNode.children,
      `track:${track}/owner:${owner}`,
      owner,
    );
    ownerNode.children.push({
      key: group.resource,
      label: leafLabel || name,
      children: [],
      group,
    });
  }

  const sortRecursively = (nodes: CredentialTreeNode[]) => {
    nodes.sort((left, right) => left.label.localeCompare(right.label));
    for (const node of nodes) sortRecursively(node.children);
  };
  sortRecursively(roots);
  return roots;
}

export function subtreeGroups(
  node: CredentialTreeNode,
): CredentialGroupData[] {
  return node.group
    ? [node.group]
    : node.children.flatMap(subtreeGroups);
}
