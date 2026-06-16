/* eslint-disable i18next/no-literal-string */
import { Braces, CheckCircle2, Code2, FileText, Server } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { CONFIDENCE_LABELS, DOMAIN_OPTIONS, NODE_TYPE_OPTIONS } from "../mock-data";
import type { Confidence } from "../types";

const CONFIDENCES = Object.keys(CONFIDENCE_LABELS) as Confidence[];

function typeIcon(label: string) {
  if (label.includes("服务")) return Server;
  if (label.includes("代码")) return Code2;
  if (label.includes("API")) return Braces;
  return FileText;
}

export function FilterSidebar({
  selectedDomain,
  onDomainChange,
}: {
  selectedDomain: string;
  onDomainChange: (domain: string) => void;
}) {
  return (
    <aside className="ug-panel ug-filter-sidebar">
      <section>
        <p className="ug-section-label">知识域</p>
        <div className="ug-sidebar-list">
          {DOMAIN_OPTIONS.map((domain) => (
            <button
              key={domain.id}
              type="button"
              onClick={() => onDomainChange(domain.id)}
              className={cn(
                "ug-sidebar-row",
                selectedDomain === domain.id
                  ? "ug-sidebar-row-active"
                  : "ug-sidebar-row-idle",
              )}
            >
              <span className={cn("ug-domain-dot", domain.color)} />
              <span className="min-w-0 flex-1 truncate">{domain.label}</span>
              <span className="ug-count-pill">{domain.count}</span>
            </button>
          ))}
        </div>
      </section>

      <section>
        <p className="ug-section-label">节点类型</p>
        <div className="ug-type-list">
          {NODE_TYPE_OPTIONS.map((label) => {
            const Icon = typeIcon(label);
            return (
              <span key={label} className="ug-type-row">
                <Icon className="size-3.5" />
                {label}
              </span>
            );
          })}
        </div>
      </section>

      <section>
        <p className="ug-section-label">可信度</p>
        <div className="ug-confidence-list">
          {CONFIDENCES.map((confidence) => (
            <span key={confidence} className={cn("ug-confidence-badge", `ug-confidence-${confidence}`)}>
              <CheckCircle2 className="size-3" />
              {CONFIDENCE_LABELS[confidence]}
            </span>
          ))}
        </div>
      </section>
    </aside>
  );
}
