"use client";

import { useMemo, useState } from "react";
import { Plus, X } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { useActorName } from "@multica/core/workspace/hooks";
import {
  useGrantAutopilotAccess,
  useRevokeAutopilotAccess,
} from "@multica/core/autopilots/mutations";
import type { AutopilotCollaborator } from "@multica/core/types";
import { toast } from "sonner";
import { ActorAvatar } from "../../common/actor-avatar";
import {
  PropertyPicker,
  PickerItem,
  PickerEmpty,
} from "../../issues/components/pickers/property-picker";
import { matchesPinyin } from "../../editor/extensions/pinyin-match";
import { useT } from "../../i18n";

// Grant / revoke explicit write access to an autopilot. Members-only, mirroring
// the subscriber picker. Creators and workspace admins always have access and
// are not listed here — this manages the additional, explicitly-granted set.
// Rendered inline inside the edit dialog's configuration sidebar; access changes
// commit immediately via their own mutations and are independent of the form's
// Save action.
export function AutopilotAccessManager({
  autopilotId,
  collaborators,
}: {
  autopilotId: string;
  collaborators: AutopilotCollaborator[];
}) {
  const { t } = useT("autopilots");
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { getActorName } = useActorName();
  const grant = useGrantAutopilotAccess();
  const revoke = useRevokeAutopilotAccess();
  const [pickerOpen, setPickerOpen] = useState(false);
  const [filter, setFilter] = useState("");

  const grantedIds = useMemo(
    () => new Set(collaborators.map((c) => c.user_id)),
    [collaborators],
  );

  const query = filter.trim().toLowerCase();
  const candidates = useMemo(
    () =>
      members.filter(
        (m) =>
          !grantedIds.has(m.user_id) &&
          (query === "" ||
            m.name.toLowerCase().includes(query) ||
            matchesPinyin(m.name, query)),
      ),
    [members, grantedIds, query],
  );

  const handleGrant = async (userId: string) => {
    try {
      await grant.mutateAsync({ autopilotId, userId });
      toast.success(t(($) => $.access.toast_granted));
    } catch (e: any) {
      toast.error(e?.message || t(($) => $.access.toast_failed));
    }
  };

  const handleRevoke = async (userId: string) => {
    try {
      await revoke.mutateAsync({ autopilotId, userId });
      toast.success(t(($) => $.access.toast_revoked));
    } catch (e: any) {
      toast.error(e?.message || t(($) => $.access.toast_failed));
    }
  };

  return (
    <div className="space-y-2">
      {collaborators.length === 0 ? (
        <p className="rounded-md border border-dashed px-3 py-3 text-center text-xs text-muted-foreground">
          {t(($) => $.access.empty)}
        </p>
      ) : (
        <ul className="space-y-1">
          {collaborators.map((c) => (
            <li
              key={c.user_id}
              className="flex items-center justify-between rounded-md border bg-background px-2 py-1.5"
            >
              <span className="flex min-w-0 items-center gap-2">
                <ActorAvatar actorType="member" actorId={c.user_id} size={20} />
                <span className="truncate text-sm">
                  {getActorName("member", c.user_id)}
                </span>
              </span>
              <button
                type="button"
                onClick={() => void handleRevoke(c.user_id)}
                disabled={revoke.isPending}
                className="cursor-pointer text-muted-foreground transition-colors hover:text-foreground disabled:opacity-50"
                aria-label={t(($) => $.access.remove_tooltip)}
              >
                <X className="size-3.5" />
              </button>
            </li>
          ))}
        </ul>
      )}

      <PropertyPicker
        open={pickerOpen}
        onOpenChange={(v) => {
          setPickerOpen(v);
          if (!v) setFilter("");
        }}
        width="w-64"
        align="start"
        searchable
        searchPlaceholder={t(($) => $.access.search_placeholder)}
        onSearchChange={setFilter}
        trigger={
          <span className="inline-flex cursor-pointer items-center gap-1 rounded-md border border-dashed px-2 py-1 text-xs text-muted-foreground transition-colors hover:border-primary/40 hover:text-foreground">
            <Plus className="size-3" />
            {t(($) => $.access.add)}
          </span>
        }
      >
        {candidates.length === 0 ? (
          <PickerEmpty />
        ) : (
          candidates.map((m) => (
            <PickerItem
              key={m.user_id}
              selected={false}
              onClick={() => {
                void handleGrant(m.user_id);
                setPickerOpen(false);
              }}
            >
              <ActorAvatar actorType="member" actorId={m.user_id} size={18} />
              <span className="truncate">{m.name}</span>
            </PickerItem>
          ))
        )}
      </PropertyPicker>

      <p className="text-[11px] text-muted-foreground">
        {t(($) => $.access.owner_note)}
      </p>
    </div>
  );
}
