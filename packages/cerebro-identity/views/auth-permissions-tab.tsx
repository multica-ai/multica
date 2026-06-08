"use client";

// FIR-2523 — Auth & Permissions settings tab. Workspace owner/admin uses this
// to control which email domains are auto-provisioned into the workspace on
// first Google login, and which role those auto-provisioned members get.
//
// Hidden entirely behind `cerebro_google_identity` (default OFF). When ON and
// the caller is not owner/admin, the form renders read-only — the backend PUT
// route already enforces the role at the middleware layer; the front-end
// gate is presentational only.

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Plus, RefreshCw, ShieldCheck, Users, X } from "lucide-react";

import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Switch } from "@multica/ui/components/ui/switch";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";

import {
  authSettingsGroupListOptions,
  authSettingsOptions,
  useUpdateAuthSettings,
} from "../queries";
import {
  SUPPORTED_DEFAULT_ROLES,
  type CerebroAuthSettingsRole,
} from "../types";

// FIR-2596 — the fixed 9 Google Workspace groups mirrored into Multica when
// the sync is on. Display-only here; the authoritative mapping lives in the
// Go worker (server/internal/cerebro/identitysync/groups.go).
const SYNCED_GROUP_NAMES = [
  "Customer Service",
  "Purchase",
  "Goods Flow",
  "Operations Specialist & Control",
  "Warehouse",
  "Sales",
  "Tech",
  "Finance",
  "MGMT",
];

export interface AuthPermissionsTabProps {}

export function AuthPermissionsTab(_props: AuthPermissionsTabProps = {}) {
  const enabled = useFeatureFlag("cerebro_google_identity");
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const currentMember = members.find((m) => m.user_id === user?.id);
  const canEdit =
    currentMember?.role === "owner" || currentMember?.role === "admin";

  const { data: settings, isPending } = useQuery(authSettingsOptions(wsId));
  const { data: groups = [], isPending: groupsPending } = useQuery(
    authSettingsGroupListOptions(wsId),
  );
  const updateMutation = useUpdateAuthSettings();

  const [domains, setDomains] = useState<string[]>([]);
  const [defaultRole, setDefaultRole] = useState<CerebroAuthSettingsRole>("member");
  const [defaultGroupId, setDefaultGroupId] = useState<string | null>(null);
  const [syncEnabled, setSyncEnabled] = useState(false);
  const [draftDomain, setDraftDomain] = useState("");

  // Re-sync form state when the query resolves OR when a save completes.
  useEffect(() => {
    if (!settings) return;
    setDomains(settings.google_signup_domains ?? []);
    const role = (SUPPORTED_DEFAULT_ROLES as readonly string[]).includes(
      settings.default_role,
    )
      ? (settings.default_role as CerebroAuthSettingsRole)
      : "member";
    setDefaultRole(role);
    setDefaultGroupId(settings.default_group_id ?? null);
    setSyncEnabled(settings.google_workspace_sync_enabled === true);
  }, [settings]);

  const dirty = useMemo(() => {
    if (!settings) return false;
    const saved = (settings.google_signup_domains ?? [])
      .map((d) => d.toLowerCase())
      .sort()
      .join(",");
    const draft = domains.map((d) => d.toLowerCase()).sort().join(",");
    return (
      saved !== draft ||
      settings.default_role !== defaultRole ||
      (settings.default_group_id ?? null) !== defaultGroupId ||
      (settings.google_workspace_sync_enabled === true) !== syncEnabled
    );
  }, [settings, domains, defaultRole, defaultGroupId, syncEnabled]);

  if (!enabled) return null;

  const handleAddDomain = () => {
    const next = draftDomain.trim().toLowerCase();
    if (!next) return;
    if (next.includes("@") || /\s/.test(next)) {
      toast.error("A domain must not contain '@' or spaces.");
      return;
    }
    if (domains.some((d) => d.toLowerCase() === next)) {
      setDraftDomain("");
      return;
    }
    setDomains([...domains, next]);
    setDraftDomain("");
  };

  const handleRemoveDomain = (target: string) => {
    setDomains(domains.filter((d) => d !== target));
  };

  const handleSave = async () => {
    try {
      await updateMutation.mutateAsync({
        workspaceId: wsId,
        google_signup_domains: domains,
        default_role: defaultRole,
        default_group_id: defaultGroupId,
        google_workspace_sync_enabled: syncEnabled,
      });
      toast.success("Auth settings saved.");
    } catch (err) {
      const message = err instanceof Error ? err.message : "Unknown error";
      toast.error(`Failed to save: ${message}`);
    }
  };

  return (
    <div className="space-y-6 max-w-2xl" data-testid="auth-permissions-tab">
      <div>
        <h3 className="text-sm font-semibold mb-1 flex items-center gap-2">
          <ShieldCheck className="h-4 w-4" />
          Auth & Permissions
        </h3>
        <p className="text-xs text-muted-foreground max-w-prose">
          Control which email domains automatically get access to this
          workspace when they log in with Google for the first time, and which
          role new members are assigned. External users (outside the
          list) still need to be invited manually from the Members tab.
        </p>
      </div>

      <section className="rounded-md border border-border p-4 space-y-3">
        <h4 className="text-sm font-medium">Auto-signup domains</h4>
        <p className="text-xs text-muted-foreground">
          When a user with one of these email domains logs in with Google
          for the first time, a membership in this workspace is automatically created.
          Leave the list empty to disable auto-signup entirely.
        </p>

        {isPending ? (
          <div className="text-xs text-muted-foreground">Loading…</div>
        ) : (
          <>
            <div className="flex flex-wrap gap-2 min-h-[1.75rem]">
              {domains.length === 0 ? (
                <span className="text-xs text-muted-foreground italic">
                  No domains — auto-signup is disabled.
                </span>
              ) : (
                domains.map((domain) => (
                  <Badge
                    key={domain}
                    variant="secondary"
                    className="gap-1 pl-2 pr-1 py-1"
                  >
                    <span>{domain}</span>
                    {canEdit && (
                      <button
                        type="button"
                        aria-label={`Remove ${domain}`}
                        onClick={() => handleRemoveDomain(domain)}
                        className="hover:bg-muted rounded"
                      >
                        <X className="h-3 w-3" />
                      </button>
                    )}
                  </Badge>
                ))
              )}
            </div>

            {canEdit && (
              <div className="flex gap-2">
                <Input
                  placeholder="firtal.com"
                  value={draftDomain}
                  onChange={(e) => setDraftDomain(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") {
                      e.preventDefault();
                      handleAddDomain();
                    }
                  }}
                  aria-label="New domain"
                  className="h-9 text-sm"
                />
                <Button
                  type="button"
                  variant="secondary"
                  size="sm"
                  onClick={handleAddDomain}
                  disabled={!draftDomain.trim()}
                >
                  <Plus className="h-4 w-4 mr-1" />
                  Add
                </Button>
              </div>
            )}
          </>
        )}
      </section>

      <section className="rounded-md border border-border p-4 space-y-3">
        <h4 className="text-sm font-medium flex items-center gap-2">
          <Users className="h-4 w-4" />
          Default group for new users
        </h4>
        <p className="text-xs text-muted-foreground">
          All new members (auto-signup, invitation, and manual addition)
          are automatically placed in this group so they have access from day one.
        </p>
        {groupsPending ? (
          <div className="text-xs text-muted-foreground">Loading groups…</div>
        ) : groups.length === 0 ? (
          <p className="text-xs text-muted-foreground italic">
            Create at least one group under the Groups tab first.
          </p>
        ) : (
          <Select
            value={defaultGroupId ?? ""}
            onValueChange={(v) => setDefaultGroupId(v || null)}
            disabled={!canEdit || isPending}
          >
            <SelectTrigger className="w-full max-w-md h-9 text-sm">
              <SelectValue placeholder="Select default group">
                {() =>
                  defaultGroupId
                    ? (groups.find((g) => g.id === defaultGroupId)?.name ?? defaultGroupId)
                    : null
                }
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              {groups.map((group) => (
                <SelectItem key={group.id} value={group.id}>
                  {group.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
      </section>

      <section className="rounded-md border border-border p-4 space-y-3">
        <h4 className="text-sm font-medium">Default role</h4>
        <p className="text-xs text-muted-foreground">
          Role that auto-created members receive. You can always change the role
          afterwards on the Members tab.
        </p>
        <Select
          value={defaultRole}
          onValueChange={(v) => setDefaultRole(v as CerebroAuthSettingsRole)}
          disabled={!canEdit || isPending}
        >
          <SelectTrigger className="w-48 h-9 text-sm">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="member">Member</SelectItem>
            <SelectItem value="admin">Admin</SelectItem>
            <SelectItem value="owner">Owner</SelectItem>
          </SelectContent>
        </Select>
      </section>

      <section className="rounded-md border border-border p-4 space-y-3">
        <div className="flex items-start justify-between gap-4">
          <div>
            <h4 className="text-sm font-medium flex items-center gap-2">
              <RefreshCw className="h-4 w-4" />
              Google Workspace group sync
            </h4>
            <p className="text-xs text-muted-foreground mt-1 max-w-prose">
              When enabled, Multica automatically keeps the 9 Firtal teams
              up to date from Google Workspace (checked every 4 hours). People who
              are added or removed from a group in Google are synced accordingly. Only
              users who are already members of this workspace are added —
              the rest are skipped.
            </p>
          </div>
          <Switch
            checked={syncEnabled}
            onCheckedChange={(v) => setSyncEnabled(v === true)}
            disabled={!canEdit || isPending}
            aria-label="Enable or disable Google Workspace group sync"
          />
        </div>
        <div className="flex flex-wrap gap-2 pt-1">
          {SYNCED_GROUP_NAMES.map((name) => (
            <Badge key={name} variant="secondary" className="py-1">
              {name}
            </Badge>
          ))}
        </div>
      </section>

      {canEdit && (
        <div className="flex justify-end">
          <Button
            type="button"
            onClick={handleSave}
            disabled={
              !dirty ||
              updateMutation.isPending ||
              (groups.length > 0 && !defaultGroupId)
            }
          >
            {updateMutation.isPending ? "Saving…" : "Save changes"}
          </Button>
        </div>
      )}

      {!canEdit && (
        <p className="text-xs text-muted-foreground italic">
          Only owners and admins can change these settings.
        </p>
      )}
    </div>
  );
}
