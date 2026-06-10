"use client";

// TECH-3156 — the "Konfigurer" button on a connection row. Drop in once per
// connection-capability row (source === "connection") on any of the five
// permission surfaces; it opens ConnectionConfigSheet with that connection's
// per-tool rows. Mirrors FirtalRegistryRowConfigure, but works on every view
// (workspace/runtime/agent/group/member), not just the agent page.
//
// TECH-3287 hul 3: when the connection-wide row is blocked by a layer this page
// cannot loosen, the button shows a lock badge so the admin sees — before
// opening — that per-tool Allow here is futile, and the sheet opens in its
// explanation mode (banner + disabled futile controls).

import { useState } from "react";
import { Lock, Settings2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  isLockedFromElsewhere,
  type ToolLayer,
  type ToolPolicyRow,
} from "../core";
import { ConnectionConfigSheet } from "./connection-config-sheet";

export function ConnectionRowConfigure({
  connectionKey,
  connectionLabel,
  connectionRow,
  toolRows,
  editLayer,
  subjectId,
  variant = "outline",
  size = "sm",
}: {
  connectionKey: string;
  connectionLabel: string;
  /**
   * The connection-wide row (source === "connection"). Its Effective verdict is
   * the floor every per-tool row inherits, so the sheet uses it to explain and
   * disable futile controls.
   */
  connectionRow: ToolPolicyRow;
  toolRows: ToolPolicyRow[];
  editLayer: ToolLayer;
  subjectId: string;
  variant?: "ghost" | "outline" | "secondary";
  size?: "sm" | "default";
}) {
  const [open, setOpen] = useState(false);
  const count = toolRows.length;
  const label = count > 0 ? `Konfigurer (${count})` : "Konfigurer";
  // The whole connection is blocked from a higher layer → flag it on the button.
  const blockedAbove = isLockedFromElsewhere(connectionRow, editLayer);

  return (
    <>
      <Button
        type="button"
        size={size}
        variant={variant}
        className="gap-1.5"
        onClick={() => setOpen(true)}
        data-testid="connection-configure"
        title={
          blockedAbove
            ? `${connectionLabel} er blokeret ovenfra — åbn for at se hvor det ændres`
            : `Configure individual tools for ${connectionLabel}`
        }
      >
        {blockedAbove ? (
          <Lock className="h-3.5 w-3.5 text-destructive" />
        ) : (
          <Settings2 className="h-3.5 w-3.5" />
        )}
        {label}
      </Button>
      <ConnectionConfigSheet
        open={open}
        onOpenChange={setOpen}
        connectionKey={connectionKey}
        connectionLabel={connectionLabel}
        connectionRow={connectionRow}
        toolRows={toolRows}
        editLayer={editLayer}
        subjectId={subjectId}
      />
    </>
  );
}
