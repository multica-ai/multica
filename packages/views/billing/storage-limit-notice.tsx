"use client";

import { useCallback } from "react";
import { useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Paperclip, X } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useAuthStore } from "@multica/core/auth";
import { memberListOptions, workspaceListOptions } from "@multica/core/workspace/queries";
import { paths, useCurrentWorkspace, useWorkspaceSlug } from "@multica/core/paths";
import {
  formatStorageBytes,
  storageLimitReachedFromError,
  workspaceSubscriptionSummaryOptions,
  type StorageLimitReached,
} from "@multica/core/billing";
import { useOptionalNavigation } from "../navigation";
import { useT } from "../i18n";

/** An upload the storage limit refused, as the notice shows it. */
export interface StorageLimitRefusal extends StorageLimitReached {
  filename: string;
  /** "copy" when a sub-issue's copy of its source attachments was refused. */
  kind?: "upload" | "copy";
}

/** The refusal behind error, or null when error is any other failure. */
export function storageLimitRefusal(error: unknown, filename: string): StorageLimitRefusal | null {
  const reached = storageLimitReachedFromError(error);
  return reached ? { ...reached, filename } : null;
}

type UpgradeKind = "upgrade_pro" | "upgrade_business" | "manage" | "view_plans";
type UpgradeAction = { kind: UpgradeKind; href: string };

/**
 * What an admin can do about a full workspace (MUL-9489). Storage grows with
 * seats, so a Business workspace adds seats in billing; a lower plan upgrades.
 * Members, and anyone outside a workspace, get no action: they ask an admin.
 */
function storageLimitAction(
  workspaceSlug: string | null,
  role: string | null,
  plan: string | undefined,
): UpgradeAction | null {
  if (!workspaceSlug || (role !== "owner" && role !== "admin")) return null;
  const billing = `${paths.workspace(workspaceSlug).settings()}?tab=billing`;
  switch (plan) {
    case "free":
      return { kind: "upgrade_pro", href: `${billing}&upgrade=pro` };
    case "pro":
      return { kind: "upgrade_business", href: `${billing}&upgrade=business` };
    case "business":
      return { kind: "manage", href: billing };
    default:
      return { kind: "view_plans", href: billing };
  }
}

function useStorageLimitCopy() {
  const { t, i18n } = useT("billing");
  const locale = i18n.language;
  return useCallback(
    (refusal: StorageLimitRefusal, action: UpgradeAction | null) => {
      const title =
        refusal.kind === "copy"
          ? t(($) => $.storage_limit.copy_title)
          : t(($) => $.storage_limit.title, { filename: refusal.filename });
      let body: string;
      if (!action) {
        body = t(($) => $.storage_limit.member_body);
      } else if (refusal.usedBytes !== null && refusal.limitBytes !== null) {
        body = t(($) => $.storage_limit.admin_body, {
          used: formatStorageBytes(refusal.usedBytes, locale),
          limit: formatStorageBytes(refusal.limitBytes, locale),
        });
      } else {
        body = t(($) => $.storage_limit.admin_body_no_figures);
      }
      const label = action
        ? {
            upgrade_pro: t(($) => $.workspace.actions.upgrade_to, { plan: t(($) => $.workspace.plan.pro) }),
            upgrade_business: t(($) => $.workspace.actions.upgrade_to, { plan: t(($) => $.workspace.plan.business) }),
            manage: t(($) => $.workspace.actions.manage),
            view_plans: t(($) => $.paid_feature.view_plans),
          }[action.kind]
        : null;
      return { title, body, label };
    },
    [locale, t],
  );
}

/** The current user's admin action, from live queries. */
function useStorageLimitAction(): UpgradeAction | null {
  const workspace = useCurrentWorkspace();
  const wsId = workspace?.id ?? "";
  const userId = useAuthStore((state) => state.user?.id ?? null);
  const members = useQuery({ ...memberListOptions(wsId), enabled: wsId.length > 0 });
  const role = members.data?.find((m) => m.user_id === userId)?.role ?? null;
  const isAdmin = role === "owner" || role === "admin";
  const summary = useQuery({ ...workspaceSubscriptionSummaryOptions(wsId), enabled: isAdmin });
  return storageLimitAction(workspace?.slug ?? null, role, summary.data?.entitlement.plan);
}

/**
 * The card a composer shows under its editor when the storage limit refused
 * a file. The file was not uploaded; everything already attached stays.
 */
export function StorageLimitNotice({
  refusal,
  onDismiss,
  className,
}: {
  refusal: StorageLimitRefusal;
  onDismiss: () => void;
  className?: string;
}) {
  const { t } = useT("billing");
  const navigation = useOptionalNavigation();
  const describe = useStorageLimitCopy();
  const action = useStorageLimitAction();
  const { title, body, label } = describe(refusal, action);
  return (
    <div
      role="alert"
      className={cn(
        "flex items-center gap-3 rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2",
        className,
      )}
    >
      <Paperclip className="size-4 shrink-0 text-muted-foreground" aria-hidden />
      <div className="min-w-0 flex-1">
        <p className="truncate text-body font-medium text-foreground">{title}</p>
        <p className="text-caption text-muted-foreground">{body}</p>
      </div>
      {action && label && navigation ? (
        <Button type="button" size="sm" onClick={() => navigation.push(action.href)}>
          {label}
        </Button>
      ) : null}
      <Button
        type="button"
        size="icon-xs"
        variant="ghost"
        aria-label={t(($) => $.storage_limit.dismiss)}
        className="text-muted-foreground"
        onClick={onDismiss}
      >
        <X />
      </Button>
    </div>
  );
}

/**
 * For uploads outside an editor (avatars, feedback, docs): returns a handler
 * that toasts the same notice and reports true for a storage refusal, false
 * for anything else so the caller shows its usual failure.
 *
 * It subscribes to no query, so a host pays nothing until a refusal happens;
 * the caller's role and plan are looked up then, from the cache when it has
 * them.
 */
export function useStorageLimitToast() {
  const client = useQueryClient();
  const slug = useWorkspaceSlug();
  const navigation = useOptionalNavigation();
  const describe = useStorageLimitCopy();
  return useCallback(
    (error: unknown, filename: string): boolean => {
      const refusal = storageLimitRefusal(error, filename);
      if (!refusal) return false;
      void resolveAction(client, slug).then((action) => {
        const { title, body, label } = describe(refusal, action);
        toast.error(title, {
          description: body,
          action:
            action && label && navigation
              ? { label, onClick: () => navigation.push(action.href) }
              : undefined,
        });
      });
      return true;
    },
    [client, describe, navigation, slug],
  );
}

async function resolveAction(client: QueryClient, slug: string | null): Promise<UpgradeAction | null> {
  if (!slug) return null;
  try {
    const workspace = client
      .getQueryData(workspaceListOptions().queryKey)
      ?.find((w) => w.slug === slug);
    const userId = useAuthStore.getState?.()?.user?.id ?? null;
    if (!workspace || !userId) return null;
    const members = await client.ensureQueryData(memberListOptions(workspace.id));
    const role = members.find((m) => m.user_id === userId)?.role ?? null;
    if (role !== "owner" && role !== "admin") return null;
    const summary = await client
      .ensureQueryData(workspaceSubscriptionSummaryOptions(workspace.id))
      .catch(() => undefined);
    return storageLimitAction(slug, role, summary?.entitlement.plan);
  } catch {
    return null;
  }
}
