"use client";

// TECH-3156 — the "Konfigurer" button on a connection row. Drop in once per
// connection-capability row (source === "connection") on any of the five
// permission surfaces; it opens ConnectionConfigSheet with that connection's
// per-tool rows. Mirrors FirtalRegistryRowConfigure, but works on every view
// (workspace/runtime/agent/group/member), not just the agent page.

import { useState } from "react";
import { Settings2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import type { ToolLayer, ToolPolicyRow } from "../core/tool-policy";
import { ConnectionConfigSheet } from "./connection-config-sheet";

export function ConnectionRowConfigure({
  connectionKey,
  connectionLabel,
  toolRows,
  editLayer,
  subjectId,
  variant = "outline",
  size = "sm",
}: {
  connectionKey: string;
  connectionLabel: string;
  toolRows: ToolPolicyRow[];
  editLayer: ToolLayer;
  subjectId: string;
  variant?: "ghost" | "outline" | "secondary";
  size?: "sm" | "default";
}) {
  const [open, setOpen] = useState(false);
  const count = toolRows.length;
  const label = count > 0 ? `Konfigurer (${count})` : "Konfigurer";

  return (
    <>
      <Button
        type="button"
        size={size}
        variant={variant}
        className="gap-1.5"
        onClick={() => setOpen(true)}
        data-testid="connection-configure"
        title={`Configure individual tools for ${connectionLabel}`}
      >
        <Settings2 className="h-3.5 w-3.5" />
        {label}
      </Button>
      <ConnectionConfigSheet
        open={open}
        onOpenChange={setOpen}
        connectionKey={connectionKey}
        connectionLabel={connectionLabel}
        toolRows={toolRows}
        editLayer={editLayer}
        subjectId={subjectId}
      />
    </>
  );
}
