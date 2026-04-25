"use client";

import { useEffect, useState } from "react";
import { Save } from "lucide-react";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { toast } from "sonner";
import { useQueryClient } from "@tanstack/react-query";
import { useCurrentWorkspace } from "@multica/core/paths";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { api } from "@multica/core/api";
import type { Workspace } from "@multica/core/types";
import { DEFAULT_AUTO_HIDE_DAYS } from "../../issues/utils/auto-hide";

export function PreferencesTab() {
  const workspace = useCurrentWorkspace();
  const qc = useQueryClient();

  const [autoHideDays, setAutoHideDays] = useState<number>(
    workspace?.settings?.auto_hide_days ?? DEFAULT_AUTO_HIDE_DAYS
  );
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setAutoHideDays(workspace?.settings?.auto_hide_days ?? DEFAULT_AUTO_HIDE_DAYS);
  }, [workspace?.settings?.auto_hide_days]);

  const handleSave = async () => {
    if (!workspace) return;
    setSaving(true);
    try {
      const updated = await api.updateWorkspace(workspace.id, {
        settings: { ...workspace.settings, auto_hide_days: autoHideDays },
      });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((ws) => (ws.id === updated.id ? updated : ws)),
      );
      toast.success("Preferências salvas");
    } catch {
      toast.error("Falha ao salvar preferências");
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-lg font-semibold">Preferências</h2>
        <p className="text-sm text-muted-foreground">
          Configurações de comportamento do workspace.
        </p>
      </div>

      <Card>
        <CardContent className="pt-6 space-y-4">
          <div>
            <h3 className="text-sm font-medium mb-3">Issues encerradas</h3>
            <div className="space-y-2">
              <Label htmlFor="auto-hide-days">
                Ocultar issues encerradas há mais de (dias)
              </Label>
              <div className="flex items-center gap-3">
                <Input
                  id="auto-hide-days"
                  type="number"
                  min={1}
                  max={365}
                  value={autoHideDays}
                  onChange={(e) => setAutoHideDays(Math.max(1, parseInt(e.target.value) || 1))}
                  className="w-24"
                />
                <span className="text-sm text-muted-foreground">dias</span>
              </div>
              <p className="text-xs text-muted-foreground">
                Issues em status terminal (done, cancelled) encerradas há mais do que esse número de dias serão ocultadas por padrão nas visualizações.
              </p>
            </div>
          </div>

          <Button onClick={handleSave} disabled={saving} size="sm">
            <Save className="h-4 w-4 mr-2" />
            {saving ? "Salvando..." : "Salvar"}
          </Button>
        </CardContent>
      </Card>
    </div>
  );
}
