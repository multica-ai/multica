"use client";

import { useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlertCircle, Loader2, RefreshCw } from "lucide-react";
import { useAuthStore } from "@multica/core/auth";
import { paths } from "@multica/core/paths";
import { workspaceListOptions } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { useNavigation } from "../navigation";
import { useT } from "../i18n";

/**
 * Landing page for Stripe Checkout and Billing Portal returns.
 *
 * Stripe only knows the return URLs a deployment configured, and a deployment
 * has exactly one set of them — so they cannot contain a workspace slug. Cloud
 * therefore appends the workspace it already authorized (`workspace_id`) plus
 * `result`, and this page turns that into the slug-based Settings URL the user
 * actually needs. Hardcoding one workspace's slug in the Stripe configuration
 * would strand every other workspace.
 *
 * Three things this page deliberately does NOT do:
 *
 *   - It never trusts a redirect target from the query string. Only
 *     `workspace_id` and `result` are read, and the destination is rebuilt from
 *     `paths`, so a crafted return link cannot become an open redirect.
 *   - It does not report success on its own. `result=success` means Stripe
 *     finished, not that the subscription is recorded; the destination page is
 *     what waits for the webhook. This page only forwards the marker.
 *   - It does not silently drop the user somewhere else when something is
 *     wrong. Paying and then landing on an unexplained page is worse than a
 *     short message, so the failure states below say what happened.
 */

const RESULT_VALUES = new Set(["success", "cancel", "portal"]);
// Same shape check the rest of core uses for route UUIDs. Only a well-formed ID
// is worth carrying anywhere, and this one can end up in a third-party OAuth
// state on the login detour below.
const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export function BillingReturnPage() {
  const { t } = useT("billing");
  const navigation = useNavigation();
  const user = useAuthStore((s) => s.user);
  const isAuthLoading = useAuthStore((s) => s.isLoading);

  const workspaceIdRaw = navigation.searchParams.get("workspace_id");
  const workspaceIdParam =
    workspaceIdRaw && UUID_RE.test(workspaceIdRaw) ? workspaceIdRaw : null;
  const resultParam = navigation.searchParams.get("result");
  const result = resultParam && RESULT_VALUES.has(resultParam) ? resultParam : null;

  const {
    data: workspaces,
    isPending: isWorkspacesPending,
    isError: isWorkspacesError,
    refetch: refetchWorkspaces,
  } = useQuery({
    ...workspaceListOptions(),
    enabled: !!user,
  });

  const match =
    workspaceIdParam && workspaces
      ? workspaces.find((ws) => ws.id === workspaceIdParam)
      : undefined;
  // Only after the list has actually arrived can "not found" mean anything.
  const isUnresolvable = !!workspaces && !match;

  useEffect(() => {
    if (isAuthLoading) return;
    // Checkout can take long enough for a session to expire, so an
    // unauthenticated return is a normal case rather than an error. Carry enough
    // to resume the return so the user does not land on their default workspace
    // with no idea whether the payment took.
    //
    // Rebuilt from the two values this page actually uses, NOT forwarded whole:
    // the login page embeds `next` verbatim in the Google OAuth `state`, so
    // anything left in here — Stripe's `session_id`, any future cloud param —
    // would be handed to a third party for no functional reason.
    // sanitizeNextUrl on the login side also accepts relative paths only, so
    // this cannot become an off-site redirect.
    if (!user) {
      const resume = new URLSearchParams();
      if (workspaceIdParam) resume.set("workspace_id", workspaceIdParam);
      if (result) resume.set("result", result);
      const query = resume.toString();
      const returnTo = query
        ? `${navigation.pathname}?${query}`
        : navigation.pathname;
      navigation.replace(`${paths.login()}?next=${encodeURIComponent(returnTo)}`);
      return;
    }
    if (isWorkspacesPending || isWorkspacesError || !workspaces) return;
    if (!match) return;

    const destination = `${paths.workspace(match.slug).settings()}?tab=billing${
      result ? `&result=${result}` : ""
    }`;
    // replace(), not push(): the Stripe hop should not sit in history where a
    // back navigation would re-run this redirect.
    navigation.replace(destination);
  }, [
    isAuthLoading,
    isWorkspacesError,
    isWorkspacesPending,
    match,
    navigation,
    result,
    user,
    workspaceIdParam,
    workspaces,
  ]);

  // The workspace list is what turns an ID into a slug, so without it there is
  // no destination to send anyone to. Retrying beats an endless spinner.
  //
  // The copy below deliberately claims nothing about the outcome. This page
  // cannot know it: `cancel` means the user backed out, `portal` may have been
  // opened and closed, and even `success` only means Stripe finished — the
  // subscription is not recorded until the webhook lands. Asserting "saved" on a
  // payment result screen is the one thing worse than saying "check Billing".
  if (isWorkspacesError) {
    return (
      <ReturnMessage
        title={t(($) => $.return_page.load_failed_title)}
        description={t(($) => $.return_page.load_failed_description)}
        action={
          <Button
            className="h-11"
            variant="outline"
            onClick={() => void refetchWorkspaces()}
          >
            <RefreshCw />
            {t(($) => $.return_page.retry)}
          </Button>
        }
      />
    );
  }

  // Deleted workspace, revoked membership, a missing or malformed workspace_id,
  // or a hand-edited link.
  if (isUnresolvable) {
    return (
      <ReturnMessage
        title={t(($) => $.return_page.workspace_missing_title)}
        description={t(($) => $.return_page.workspace_missing_description)}
        action={
          <Button className="h-11" onClick={() => navigation.replace(paths.root())}>
            {t(($) => $.return_page.go_to_app)}
          </Button>
        }
      />
    );
  }

  return (
    <div
      className="flex min-h-dvh items-center justify-center bg-background p-6"
      role="status"
      aria-live="polite"
    >
      <div className="flex items-center gap-3 text-body text-muted-foreground">
        <Loader2 className="size-4 animate-spin motion-reduce:animate-none" />
        <span>{t(($) => $.return_page.redirecting)}</span>
      </div>
    </div>
  );
}

function ReturnMessage({
  title,
  description,
  action,
}: {
  title: string;
  description: string;
  action: React.ReactNode;
}) {
  return (
    <div className="flex min-h-dvh items-center justify-center bg-background p-6">
      <div
        className="flex max-w-[60ch] flex-col items-start gap-3"
        role="alert"
        aria-live="polite"
      >
        <div className="flex items-center gap-2 text-body font-medium">
          <AlertCircle className="size-4 text-destructive" />
          <span>{title}</span>
        </div>
        <p className="text-caption leading-5 text-muted-foreground">
          {description}
        </p>
        {action}
      </div>
    </div>
  );
}
