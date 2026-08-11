import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { ProductStatusBadge, ProductNodeDetail } from "./index";
import type { ProductMapNode } from "@multica/core/products";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: undefined, isLoading: false }),
}));

const releasedNode: ProductMapNode = {
  id: "n1",
  workspace_id: "ws-1",
  name: "Multica",
  slug: "multica",
  description: "desc",
  sort_order: 1,
  status: "released",
  status_source: "code_repo",
  evidence: { source: "code_repo", repo_url: "https://gitlab.sy.soyoung.com/fe/wasai/multica.git" },
  created_at: "2026-08-11T00:00:00Z",
  updated_at: "2026-08-11T00:00:00Z",
  refs: [{ ref_type: "project", ref_id: "p1" }],
  editors: [{ user_id: "u1" }],
  has_live_evidence: true,
};

const pendingNode: ProductMapNode = {
  ...releasedNode,
  id: "n2",
  name: "院务系统",
  slug: "yuanwu",
  status: "pending_confirmation",
  status_source: "pmo",
  evidence: { source: "pmo", note: "no pmo data yet" },
  has_live_evidence: false,
};

describe("ProductStatusBadge", () => {
  it("labels released with evidence as 已上线", () => {
    render(<ProductStatusBadge node={releasedNode} />);
    expect(screen.getByText(/已上线/)).toBeTruthy();
    expect(screen.getByText(/有证据/)).toBeTruthy();
  });

  it("labels evidence-less node as 待确认", () => {
    render(<ProductStatusBadge node={pendingNode} />);
    expect(screen.getByText(/待确认/)).toBeTruthy();
  });
});

describe("ProductNodeDetail", () => {
  const sourceLabels = { pmo: "PMO 上线状态", code_repo: "代码仓库" };

  it("shows live evidence for a released code_repo node", () => {
    render(<ProductNodeDetail node={releasedNode} statusSourceLabel={sourceLabels} />);
    expect(screen.getByText("Multica")).toBeTruthy();
    expect(screen.getByText(/代码仓库/)).toBeTruthy();
    expect(screen.getByText(/gitlab.sy.soyoung.com/)).toBeTruthy();
  });

  it("shows 待确认 and does not claim live for a node with no evidence", () => {
    render(<ProductNodeDetail node={pendingNode} statusSourceLabel={sourceLabels} />);
    expect(screen.getByText("院务系统")).toBeTruthy();
    expect(screen.getAllByText(/待确认/).length).toBeGreaterThan(0);
    expect(screen.queryByText(/已上线/)).toBeNull();
  });

  it("shows an empty-state placeholder when no node is selected", () => {
    render(<ProductNodeDetail node={null} statusSourceLabel={sourceLabels} />);
    expect(screen.getByText(/选择左侧产品节点查看详情/)).toBeTruthy();
  });
});
