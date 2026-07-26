import { describe, expect, it } from "vitest";
import {
  buildCredentialTree,
  subtreeGroups,
  type CredentialTreeNode,
} from "./tool-policy-table";

// A CredentialGroupData is { resource, label, rows }; the tree only reads
// resource + label, so rows can be an empty stand-in here.
function box(label: string) {
  return { resource: `agentvault-vault:${label}`, label, rows: [] as never[] };
}

function labels(nodes: CredentialTreeNode[]): unknown {
  return nodes.map((n) =>
    n.group ? n.label : { [n.label]: labels(n.children) },
  );
}

describe("buildCredentialTree (FIR-2441)", () => {
  it("nests <track>-<owner>-<credential> into track › owner › credential", () => {
    const tree = buildCredentialTree([
      box("agents-mia-supabase"),
      box("agents-mia-agent-vault"),
      box("members-jesper-bigquery"),
    ]);
    expect(labels(tree)).toEqual([
      { agents: [{ mia: ["agent-vault", "supabase"] }] },
      { members: [{ jesper: ["bigquery"] }] },
    ]);
  });

  it("keeps multi-dash credential names intact as the leaf label", () => {
    const tree = buildCredentialTree([box("agents-mia-s3-engeni-buckets")]);
    // agents › mia › "s3-engeni-buckets" (only the first two dashes are structural)
    const owner = tree[0]!.children[0]!;
    expect(owner.label).toBe("mia");
    expect(owner.children[0]!.label).toBe("s3-engeni-buckets");
  });

  it("leaves non-convention names (e.g. default, no track) as top-level leaves", () => {
    const tree = buildCredentialTree([
      box("default"),
      box("agents-mia-supabase"),
    ]);
    // "default" is a top-level leaf, sorted before the "agents" branch
    expect(tree[0]!.label).toBe("agents");
    expect(tree[1]!.group?.label).toBe("default");
  });

  it("subtreeGroups collects every leaf box under a branch (cascade target)", () => {
    const tree = buildCredentialTree([
      box("agents-mia-supabase"),
      box("agents-mia-agent-vault"),
      box("members-jesper-bigquery"),
    ]);
    const agentsBranch = tree[0]!;
    expect(subtreeGroups(agentsBranch).map((g) => g.label).sort()).toEqual([
      "agents-mia-agent-vault",
      "agents-mia-supabase",
    ]);
  });
});
