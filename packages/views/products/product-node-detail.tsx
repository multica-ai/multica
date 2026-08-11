"use client";

import type { ProductMapNode } from "@multica/core/products";
import { Card } from "@multica/ui/components/ui/card";
import { Badge } from "@multica/ui/components/ui/badge";

import { ProductStatusBadge } from "./product-map-page";

export function ProductNodeDetail({
  node,
  statusSourceLabel,
}: {
  node: ProductMapNode | null;
  statusSourceLabel: Record<string, string>;
}) {
  if (!node) {
    return (
      <Card className="p-6 text-sm text-muted-foreground">
        选择左侧产品节点查看详情
      </Card>
    );
  }

  const evidenceEntries = Object.entries(node.evidence ?? {});

  return (
    <Card className="p-6">
      <div className="flex items-center justify-between gap-4">
        <div>
          <h2 className="text-lg font-semibold">{node.name}</h2>
          <p className="text-xs text-muted-foreground">
            slug: {node.slug} · 状态来源: {statusSourceLabel[node.status_source] ?? node.status_source}
          </p>
        </div>
        <ProductStatusBadge node={node} />
      </div>

      {node.description && (
        <p className="mt-4 text-sm text-muted-foreground">{node.description}</p>
      )}

      <div className="mt-6 grid grid-cols-2 gap-6">
        <div>
          <h3 className="text-sm font-semibold">关联项目 / 需求</h3>
          {node.refs.length === 0 ? (
            <p className="mt-2 text-sm text-muted-foreground">
              暂无追溯链接（项目进入工作区后回填）
            </p>
          ) : (
            <ul className="mt-2 space-y-1 text-sm">
              {node.refs.map((ref) => (
                <li key={`${ref.ref_type}-${ref.ref_id}`} className="flex items-center gap-2">
                  <Badge variant="outline">{ref.ref_type}</Badge>
                  <code className="text-xs">{ref.ref_id}</code>
                </li>
              ))}
            </ul>
          )}
        </div>

        <div>
          <h3 className="text-sm font-semibold">上线状态证据</h3>
          {node.has_live_evidence && evidenceEntries.length > 0 ? (
            <dl className="mt-2 space-y-1 text-sm">
              {evidenceEntries.map(([k, v]) => (
                <div key={k} className="flex gap-2">
                  <dt className="text-muted-foreground">{k}:</dt>
                  <dd className="truncate font-mono text-xs">
                    {typeof v === "string" ? v : JSON.stringify(v)}
                  </dd>
                </div>
              ))}
            </dl>
          ) : (
            <p className="mt-2 text-sm text-muted-foreground">
              待确认 —— 无 PMO / 代码仓库证据时不以 Issue 状态推断上线
            </p>
          )}
        </div>
      </div>

      <p className="mt-6 text-xs text-muted-foreground">
        更新时间: {new Date(node.updated_at).toLocaleString()}
        {node.editors.length > 0 &&
          ` · 产品编辑人: ${node.editors.map((e) => e.user_id).join(", ")}`}
      </p>
    </Card>
  );
}
