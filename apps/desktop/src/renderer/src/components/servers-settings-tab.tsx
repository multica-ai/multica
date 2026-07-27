import { useCallback, useEffect, useState } from "react";
import { Check, Loader2, Pencil, Plus, Trash2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { useT } from "@multica/views/i18n";
import { SettingsCard, SettingsRow, SettingsTab } from "@multica/views/settings";
import { toast } from "sonner";
import type { DesktopServerProfile, DesktopServersState } from "../../../shared/runtime-config";
import { switchDesktopServer } from "../platform/server-switch";

type FormMode =
  | { kind: "closed" }
  | { kind: "add" }
  | { kind: "edit"; server: DesktopServerProfile };

export function ServersSettingsTab() {
  const { t } = useT("settings");
  const [servers, setServers] = useState<DesktopServersState | null>(null);
  const [loading, setLoading] = useState(true);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [formMode, setFormMode] = useState<FormMode>({ kind: "closed" });
  const [formName, setFormName] = useState("");
  const [formApiUrl, setFormApiUrl] = useState("");
  const [formAppUrl, setFormAppUrl] = useState("");
  const [saving, setSaving] = useState(false);

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const result = await window.desktopAPI.listServers();
      if (result.ok) {
        setServers(result.servers);
      } else {
        toast.error(result.error);
      }
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const closeForm = useCallback(() => {
    setFormMode({ kind: "closed" });
    setFormName("");
    setFormApiUrl("");
    setFormAppUrl("");
  }, []);

  const openAdd = useCallback(() => {
    setFormName("");
    setFormApiUrl("");
    setFormAppUrl("");
    setFormMode({ kind: "add" });
  }, []);

  const openEdit = useCallback((server: DesktopServerProfile) => {
    setFormName(server.name);
    setFormApiUrl(server.apiUrl);
    setFormAppUrl(
      // Only surface appUrl when it differs from the default derivation
      // shown as placeholder — keep the field empty when it's auto-derived
      // so save doesn't pin a redundant value. Still allow explicit edit.
      server.appUrl,
    );
    setFormMode({ kind: "edit", server });
  }, []);

  const handleSwitch = useCallback(
    async (server: DesktopServerProfile) => {
      if (!servers || server.id === servers.activeServerId) return;
      setBusyId(server.id);
      try {
        await switchDesktopServer(server.id);
      } catch (err) {
        toast.error(err instanceof Error ? err.message : String(err));
        setBusyId(null);
      }
    },
    [servers],
  );

  const handleRemove = useCallback(
    async (server: DesktopServerProfile) => {
      if (!servers) return;
      if (servers.servers.length <= 1) {
        toast.error(t(($) => $.desktop.servers.cannot_remove_last));
        return;
      }
      setBusyId(server.id);
      try {
        const result = await window.desktopAPI.removeServer(server.id);
        if (!result.ok) {
          toast.error(result.error);
          return;
        }
        setServers(result.servers);
        if (formMode.kind === "edit" && formMode.server.id === server.id) {
          closeForm();
        }
        // If we removed the active server, main already rewrote activeServerId;
        // reload so the app binds to the new active endpoints.
        if (server.id === servers.activeServerId) {
          await switchDesktopServer(result.servers.activeServerId);
          return;
        }
        toast.success(t(($) => $.desktop.servers.removed));
      } catch (err) {
        toast.error(err instanceof Error ? err.message : String(err));
      } finally {
        setBusyId(null);
      }
    },
    [closeForm, formMode, servers, t],
  );

  const handleSave = useCallback(async () => {
    if (!formApiUrl.trim()) {
      toast.error(t(($) => $.desktop.servers.api_url_required));
      return;
    }
    if (!servers) return;

    setSaving(true);
    try {
      const editing = formMode.kind === "edit" ? formMode.server : null;
      const wasActive = editing
        ? editing.id === servers.activeServerId
        : false;
      const endpointChanged = editing
        ? normalizeComparableUrl(editing.apiUrl) !==
            normalizeComparableUrl(formApiUrl.trim()) ||
          (formAppUrl.trim() !== "" &&
            normalizeComparableUrl(editing.appUrl) !==
              normalizeComparableUrl(formAppUrl.trim()))
        : false;

      const result = await window.desktopAPI.upsertServer({
        id: editing?.id,
        name: formName.trim() || formApiUrl.trim(),
        apiUrl: formApiUrl.trim(),
        appUrl: formAppUrl.trim() || undefined,
      });
      if (!result.ok) {
        toast.error(result.error);
        return;
      }
      setServers(result.servers);
      closeForm();

      if (wasActive && endpointChanged) {
        // Active endpoint shape changed — reload so API/WS clients rebind.
        toast.success(t(($) => $.desktop.servers.updated_reloading));
        await switchDesktopServer(editing!.id);
        return;
      }

      toast.success(
        editing
          ? t(($) => $.desktop.servers.updated)
          : t(($) => $.desktop.servers.added),
      );
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  }, [closeForm, formApiUrl, formAppUrl, formMode, formName, servers, t]);

  const editable = servers?.editable !== false;
  const formOpen = formMode.kind !== "closed";

  return (
    <SettingsTab
      title={t(($) => $.desktop.servers.title)}
      description={t(($) => $.desktop.servers.description)}
    >
      {!editable && (
        <p className="rounded-md border border-dashed bg-muted/40 px-3 py-2 text-xs text-muted-foreground">
          {t(($) => $.desktop.servers.dev_mode_hint)}
        </p>
      )}

      <SettingsCard>
        {loading || !servers ? (
          <SettingsRow label={t(($) => $.desktop.servers.loading)}>
            <Loader2 className="size-4 animate-spin text-muted-foreground" />
          </SettingsRow>
        ) : (
          servers.servers.map((server) => {
            const isActive = server.id === servers.activeServerId;
            const busy = busyId === server.id;
            const isEditing =
              formMode.kind === "edit" && formMode.server.id === server.id;
            return (
              <SettingsRow
                key={server.id}
                label={
                  <span className="inline-flex items-center gap-2">
                    {server.name}
                    {isActive ? (
                      <span className="inline-flex items-center gap-1 rounded-full bg-success/10 px-1.5 py-0.5 text-[10px] font-medium text-success">
                        <Check className="size-3" />
                        {t(($) => $.desktop.servers.active)}
                      </span>
                    ) : null}
                  </span>
                }
                description={
                  <span className="font-mono text-[11px] break-all">
                    {server.apiUrl}
                  </span>
                }
                align="start"
              >
                <div className="flex items-center gap-1.5">
                  {!isActive && (
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={!editable || busy}
                      onClick={() => void handleSwitch(server)}
                    >
                      {busy ? (
                        <Loader2 className="size-3.5 animate-spin" />
                      ) : (
                        t(($) => $.desktop.servers.switch)
                      )}
                    </Button>
                  )}
                  <Button
                    variant="ghost"
                    size="sm"
                    disabled={!editable || busy || isEditing}
                    onClick={() => openEdit(server)}
                    aria-label={t(($) => $.desktop.servers.edit)}
                  >
                    <Pencil className="size-3.5 text-muted-foreground" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    disabled={!editable || busy || servers.servers.length <= 1}
                    onClick={() => void handleRemove(server)}
                    aria-label={t(($) => $.desktop.servers.remove)}
                  >
                    <Trash2 className="size-3.5 text-muted-foreground" />
                  </Button>
                </div>
              </SettingsRow>
            );
          })
        )}
      </SettingsCard>

      {editable && (
        <SettingsCard>
          {!formOpen ? (
            <SettingsRow
              label={t(($) => $.desktop.servers.add_title)}
              description={t(($) => $.desktop.servers.add_description)}
            >
              <Button variant="outline" size="sm" onClick={openAdd}>
                <Plus className="size-3.5" />
                {t(($) => $.desktop.servers.add)}
              </Button>
            </SettingsRow>
          ) : (
            <div className="space-y-3 px-4 py-4">
              <p className="text-sm font-medium">
                {formMode.kind === "edit"
                  ? t(($) => $.desktop.servers.edit_title)
                  : t(($) => $.desktop.servers.add_title)}
              </p>
              <div className="space-y-1.5">
                <label className="text-xs font-medium text-muted-foreground">
                  {t(($) => $.desktop.servers.name_label)}
                </label>
                <Input
                  value={formName}
                  onChange={(e) => setFormName(e.target.value)}
                  placeholder={t(($) => $.desktop.servers.name_placeholder)}
                />
              </div>
              <div className="space-y-1.5">
                <label className="text-xs font-medium text-muted-foreground">
                  {t(($) => $.desktop.servers.api_url_label)}
                </label>
                <Input
                  value={formApiUrl}
                  onChange={(e) => setFormApiUrl(e.target.value)}
                  placeholder="https://api.example.com"
                  autoComplete="off"
                />
              </div>
              <div className="space-y-1.5">
                <label className="text-xs font-medium text-muted-foreground">
                  {t(($) => $.desktop.servers.app_url_label)}
                </label>
                <Input
                  value={formAppUrl}
                  onChange={(e) => setFormAppUrl(e.target.value)}
                  placeholder={t(($) => $.desktop.servers.app_url_placeholder)}
                  autoComplete="off"
                />
              </div>
              {formMode.kind === "edit" &&
                formMode.server.id === servers?.activeServerId && (
                  <p className="text-xs text-muted-foreground">
                    {t(($) => $.desktop.servers.edit_active_hint)}
                  </p>
                )}
              <div className="flex justify-end gap-2 pt-1">
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={saving}
                  onClick={closeForm}
                >
                  {t(($) => $.desktop.servers.cancel)}
                </Button>
                <Button size="sm" disabled={saving} onClick={() => void handleSave()}>
                  {saving ? (
                    <Loader2 className="size-3.5 animate-spin" />
                  ) : (
                    t(($) => $.desktop.servers.save)
                  )}
                </Button>
              </div>
            </div>
          )}
        </SettingsCard>
      )}
    </SettingsTab>
  );
}

function normalizeComparableUrl(value: string): string {
  try {
    const url = new URL(value.trim());
    url.hash = "";
    url.search = "";
    return url.toString().replace(/\/+$/, "");
  } catch {
    return value.trim().replace(/\/+$/, "");
  }
}
