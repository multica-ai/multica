"use client";

import { Code, Briefcase, Layers } from "lucide-react";

const MODE_OPTIONS = [
  { value: "coding" as const, label: "Coding", icon: Code, description: "Writes code, checks out repos" },
  { value: "operational" as const, label: "Operational", icon: Briefcase, description: "Business tasks via MCP tools" },
  { value: "hybrid" as const, label: "Hybrid", icon: Layers, description: "Code + operational tasks" },
] as const;

type AgentMode = "coding" | "operational" | "hybrid";

export function ModePicker({
  value,
  canEdit,
  onChange,
}: {
  value: AgentMode;
  canEdit: boolean;
  onChange: (v: AgentMode) => void;
}) {
  const current = MODE_OPTIONS.find((o) => o.value === value) ?? MODE_OPTIONS[0];
  const Icon = current.icon;

  if (!canEdit) {
    return (
      <div className="flex items-center gap-1.5 text-sm text-muted-foreground">
        <Icon className="h-3.5 w-3.5" />
        <span>{current.label}</span>
      </div>
    );
  }

  return (
    <div className="flex gap-1">
      {MODE_OPTIONS.map((opt) => {
        const OptIcon = opt.icon;
        const selected = opt.value === value;
        return (
          <button
            key={opt.value}
            type="button"
            onClick={() => onChange(opt.value)}
            title={opt.description}
            className={`flex items-center gap-1 rounded px-2 py-0.5 text-xs transition-colors ${
              selected
                ? "bg-primary/10 text-primary font-medium"
                : "text-muted-foreground hover:bg-muted"
            }`}
          >
            <OptIcon className="h-3 w-3" />
            {opt.label}
          </button>
        );
      })}
    </div>
  );
}
