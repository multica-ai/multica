"use client";
// Admin editor for the Infisical folders a member is allowed to grant to their
// agents. The agent Infisical tab can only pick folders that appear here, so
// this card is the gate's source of truth (FIR-2143).

import { useEffect, useState } from "react";
import { Loader2, Plus, Save, Trash2 } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import type { AgentInfisicalFolder } from "@multica/core/types";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { toast } from "sonner";

let nextRowId = 0;

type Row = AgentInfisicalFolder & { row_id: number };

function toRows(folders: AgentInfisicalFolder[]): Row[] {
  return folders.map((f) => ({ ...f, row_id: nextRowId++ }));
}

function toFolders(rows: Row[]): AgentInfisicalFolder[] {
  return rows
    .map((r) => ({
      environment: r.environment.trim(),
      secret_path: r.secret_path.trim() || "/",
    }))
    .filter((r) => r.environment);
}

export function MemberInfisicalFoldersCard({
  wsId,
  memberId,
  memberName,
}: {
  wsId: string;
  memberId: string;
  memberName: string;
}) {
  const { data, isLoading } = useQuery({
    queryKey: ["workspace", wsId, "members", memberId, "infisical-folders"],
    queryFn: () => api.listMemberInfisicalFolders(wsId, memberId),
  });

  const [rows, setRows] = useState<Row[]>([]);
  const [original, setOriginal] = useState<AgentInfisicalFolder[]>([]);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!data) return;
    setRows(toRows(data));
    setOriginal(toFolders(toRows(data)));
  }, [data]);

  const current = toFolders(rows);
  const dirty = JSON.stringify(current) !== JSON.stringify(original);

  const addRow = () =>
    setRows([...rows, { row_id: nextRowId++, environment: "production", secret_path: "/" }]);

  const updateRow = (index: number, field: keyof AgentInfisicalFolder, value: string) =>
    setRows(rows.map((r, i) => (i === index ? { ...r, [field]: value } : r)));

  const removeRow = (index: number) => setRows(rows.filter((_, i) => i !== index));

  const handleSave = async () => {
    const keys = current.map((f) => `${f.environment} ${f.secret_path}`);
    if (new Set(keys).size !== keys.length) {
      toast.error("Each folder can only be added once");
      return;
    }
    setSaving(true);
    try {
      const saved = await api.replaceMemberInfisicalFolders(wsId, memberId, current);
      setRows(toRows(saved));
      setOriginal(toFolders(toRows(saved)));
      toast.success("Allowed Infisical folders saved");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not save allowed folders");
    } finally {
      setSaving(false);
    }
  };

  return (
    <Card data-testid="member-infisical-folders-card">
      <CardContent className="space-y-3">
        <div className="flex items-start justify-between gap-3">
          <div>
            <h2 className="text-sm font-medium">Allowed Infisical folders</h2>
            <p className="mt-1 text-xs text-muted-foreground">
              Folders {memberName} may grant to their agents. An agent can only be
              given — and only starts with — folders that appear in this list.
            </p>
          </div>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={addRow}
            className="shrink-0"
          >
            <Plus className="h-3 w-3" />
            Add
          </Button>
        </div>

        {isLoading ? (
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
            Loading…
          </div>
        ) : rows.length > 0 ? (
          <div className="space-y-2">
            <div className="grid gap-2 md:grid-cols-[160px_1fr_32px] md:items-center">
              <span className="text-xs font-medium text-muted-foreground">Environment</span>
              <span className="text-xs font-medium text-muted-foreground">Folder path</span>
              <span />
            </div>
            {rows.map((row, index) => (
              <div
                key={row.row_id}
                className="grid gap-2 md:grid-cols-[160px_1fr_32px] md:items-center"
              >
                <Input
                  value={row.environment}
                  onChange={(e) => updateRow(index, "environment", e.target.value)}
                  placeholder="production"
                  className="font-mono text-xs"
                />
                <Input
                  value={row.secret_path}
                  onChange={(e) => updateRow(index, "secret_path", e.target.value)}
                  placeholder="/"
                  className="font-mono text-xs"
                />
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  onClick={() => removeRow(index)}
                  className="text-muted-foreground hover:text-destructive"
                  aria-label="Remove allowed folder"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </Button>
              </div>
            ))}
          </div>
        ) : (
          <p className="text-xs italic text-muted-foreground">
            {memberName} is not allowed any Infisical folders yet.
          </p>
        )}

        <div className="flex items-center justify-end gap-3">
          {dirty && (
            <span className="text-xs text-muted-foreground">Unsaved changes</span>
          )}
          <Button onClick={handleSave} disabled={!dirty || saving} size="sm">
            {saving ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <Save className="h-3.5 w-3.5" />
            )}
            Save
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
