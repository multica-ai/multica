"use client";

import { useState } from "react";
import { Check, X } from "lucide-react";

import { Input } from "@multica/ui/components/ui/input";
import { Badge } from "@multica/ui/components/ui/badge";

import type { AuditEntry } from "../types";

export function AuditLogView({ entries }: { entries: AuditEntry[] }) {
  const [filter, setFilter] = useState("");

  const visible = entries.filter((e) => {
    if (!filter) return true;
    const q = filter.toLowerCase();
    return (
      e.actor_label.toLowerCase().includes(q) ||
      e.action.toLowerCase().includes(q) ||
      (e.reason?.toLowerCase().includes(q) ?? false)
    );
  });

  return (
    <div className="space-y-3">
      <Input
        type="search"
        placeholder="Filter by actor, action, or reason…"
        value={filter}
        onChange={(e) => setFilter(e.target.value)}
        className="max-w-md"
      />
      {visible.length === 0 ? (
        <p className="py-6 text-center text-sm text-muted-foreground">
          No audit entries.
        </p>
      ) : (
        <ul className="divide-y rounded border" data-testid="audit-log">
          {visible.map((e) => (
            <li key={e.id} className="flex items-center gap-3 p-2 text-sm">
              <span aria-hidden>
                {e.outcome === "allow" ? (
                  <Check className="size-4 text-emerald-600" />
                ) : (
                  <X className="size-4 text-destructive" />
                )}
              </span>
              <Badge variant="outline" className="font-mono text-[10px]">
                {e.action}
              </Badge>
              <span className="font-medium">{e.actor_label}</span>
              <span className="text-xs text-muted-foreground">
                ({e.actor_kind})
              </span>
              {e.reason ? (
                <span className="text-xs text-muted-foreground">
                  · {e.reason}
                </span>
              ) : null}
              <span className="ml-auto text-xs text-muted-foreground">
                {new Date(e.occurred_at).toLocaleString()}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
