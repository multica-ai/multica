"use client";

import { Sparkles } from "@multica/ui/icons";
import type { PaidFeature } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import { useWorkspacePaths } from "@multica/core/paths";
import { Badge } from "@multica/ui/components/ui/badge";
import { buttonVariants } from "@multica/ui/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { cn } from "@multica/ui/lib/utils";
import { AppLink } from "../navigation";
import { useT } from "../i18n";

/** Business features whose locked option sits in a picker next to free ones. */
export type UpgradeBadgeFeature = Extract<
  PaidFeature,
  "agent_invocation_members" | "runtime_member_sharing"
>;

/**
 * The "Business" badge beside a locked paid option (MUL-9416). It is its own
 * focusable button, never part of the option's label, and opens a card that
 * says what the feature is and how to get it: owners and admins go to
 * Billing, other members are told to ask an admin.
 */
export function PaidFeatureUpgradeBadge({
  feature,
  className,
}: {
  feature: UpgradeBadgeFeature;
  className?: string;
}) {
  const { t } = useT("billing");
  return (
    <Popover>
      <PopoverTrigger
        aria-label={t(($) => $.paid_feature.badge_label)}
        className={cn(
          "inline-flex h-5 shrink-0 items-center gap-1 rounded-md bg-brand/10 px-1.5 text-caption font-medium text-brand transition-colors hover:bg-brand/15 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
          className,
        )}
      >
        <Sparkles className="size-3" aria-hidden="true" />
        {t(($) => $.paid_feature.business)}
      </PopoverTrigger>
      <PopoverContent align="end" className="w-72 gap-2 p-3">
        <UpgradeCard feature={feature} />
      </PopoverContent>
    </Popover>
  );
}

// Mounted only while the card is open, so the role lookup (auth store and
// member list) runs for people who look, not for every locked picker.
function UpgradeCard({ feature }: { feature: UpgradeBadgeFeature }) {
  const { t } = useT("billing");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { role } = useCurrentMember(wsId);
  const isManager = role === "owner" || role === "admin";
  return (
    <>
      <Badge variant="secondary">{t(($) => $.paid_feature.business)}</Badge>
      <p className="text-body font-medium">
        {t(($) => $.paid_feature.upgrade_card[feature].title)}
      </p>
      <p className="text-caption text-muted-foreground">
        {t(($) => $.paid_feature.upgrade_card[feature].description)}
      </p>
      {isManager ? (
        <AppLink
          href={`${paths.settings()}?tab=billing`}
          className={cn(buttonVariants({ size: "sm" }), "mt-1 self-start")}
        >
          {t(($) => $.paid_feature.upgrade_to_business)}
        </AppLink>
      ) : role !== null ? (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.paid_feature.ask_admin)}
        </p>
      ) : null}
    </>
  );
}
