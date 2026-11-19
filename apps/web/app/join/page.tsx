"use client";

import { useEffect, useState, Suspense } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { useQueryClient } from "@tanstack/react-query";
import { api, errorCode } from "@multica/core/api";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Badge } from "@multica/ui/components/ui/badge";
import { useAuthStore } from "@multica/core/auth";
import { isMemberLimitReachedError } from "@multica/core/billing";
import { workspaceKeys } from "@multica/core/workspace/queries";
import type { ShareLinkInfo, Workspace } from "@multica/core/types";
import { useT } from "@multica/views/i18n";

function JoinInner() {
  const { t } = useT("invite");
  const router = useRouter();
  const searchParams = useSearchParams();
  const code = searchParams.get("code");
  const user = useAuthStore((s) => s.user);
  const queryClient = useQueryClient();

  const [info, setInfo] = useState<ShareLinkInfo | null>(null);
  const [infoError, setInfoError] = useState<string | null>(null);
  const [joining, setJoining] = useState(false);
  const [joinError, setJoinError] = useState<string | null>(null);
  const [joined, setJoined] = useState(false);
  // The workspace has no free member slot; the link works again once it
  // upgrades.
  const [workspaceFull, setWorkspaceFull] = useState(false);

  // Load the share-link preview once. No auth needed: this endpoint only
  // exposes the workspace name/slug and the inviter.
  useEffect(() => {
    if (!code) {
      setInfoError(t(($) => $.join.no_code));
      return;
    }
    let cancelled = false;
    api
      .getShareLinkInfo(code)
      .then((data) => {
        if (!cancelled) setInfo(data);
      })
      .catch(() => {
        if (!cancelled) setInfoError(t(($) => $.join.invalid));
      });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- load the preview once per code
  }, [code]);

  const handleJoin = () => {
    if (!code) return;
    if (!user) {
      // Not logged in: send them to login, then return here to join.
      router.push(`/login?next=${encodeURIComponent(`/join?code=${code}`)}`);
      return;
    }
    setJoining(true);
    setJoinError(null);
    // A retry shows its own outcome: the link may have been revoked since.
    setWorkspaceFull(false);
    api
      .joinByShareLink(code)
      .then(async (result) => {
        setJoined(true);
        const list = await api.listWorkspaces().catch(() => [] as Workspace[]);
        queryClient.setQueryData(workspaceKeys.list(), list);
        setTimeout(() => {
          router.push(`/${result.workspace_slug || result.workspace_id}/issues`);
        }, 1200);
      })
      .catch(async (e) => {
        const code = errorCode(e);
        const msg = e instanceof Error ? e.message : "";
        if (msg.includes("already a member")) {
          // Already joined: go straight to the workspace this invite points at
          // rather than the account's first workspace.
          if (info?.workspace_slug) {
            router.push(`/${info.workspace_slug}/issues`);
            return;
          }
          try {
            const workspaces = await api.listWorkspaces();
            queryClient.setQueryData(workspaceKeys.list(), workspaces as any);
            const first = workspaces[0];
            if (first) {
              router.push(`/${first.slug}/issues`);
              return;
            }
          } catch {
            // Fall through to the home redirect below.
          }
          router.push("/");
          return;
        }
        setJoining(false);
        if (isMemberLimitReachedError(e)) {
          setWorkspaceFull(true);
          return;
        }
        if (code === "seat_capacity_full") {
          setJoinError(t(($) => $.join.seats_full));
          return;
        }
        if (code === "seat_capacity_unavailable") {
          setJoinError(t(($) => $.join.seats_unavailable));
          return;
        }
        setJoinError(msg || t(($) => $.join.join_failed));
      });
  };

  return (
    <div className="flex min-h-screen items-center justify-center bg-muted/30 p-4">
      <Card className="w-full max-w-md">
        <CardContent className="space-y-4 pt-6">
          {joined ? (
            <>
              <h1 className="text-title-lg font-semibold text-center">{t(($) => $.join.joined_title)}</h1>
              <p className="text-center text-muted-foreground">{t(($) => $.join.redirecting)}</p>
            </>
          ) : infoError ? (
            <>
              <h1 className="text-title-lg font-semibold text-center">{t(($) => $.join.error_title)}</h1>
              <p className="text-center text-muted-foreground">{infoError}</p>
              <div className="flex justify-center pt-2">
                <Button variant="outline" onClick={() => router.push("/")}>
                  {t(($) => $.join.go_home)}
                </Button>
              </div>
            </>
          ) : !info ? (
            <div className="py-6 text-center text-muted-foreground">{t(($) => $.join.loading)}</div>
          ) : workspaceFull ? (
            <>
              <h1 className="text-title-lg font-semibold text-center">
                {t(($) => $.join.full_title, { workspace: info.workspace_name })}
              </h1>
              <p className="text-center text-muted-foreground">{t(($) => $.join.full_description)}</p>
              {/* A slot can free up without the link changing (an invitation
                  revoked, a member removed), so the join stays one click away. */}
              <div className="flex justify-center pt-2">
                <Button variant="outline" onClick={handleJoin} disabled={joining}>
                  {joining ? t(($) => $.join.joining) : t(($) => $.join.retry)}
                </Button>
              </div>
            </>
          ) : (
            <>
              <h1 className="text-title-lg font-semibold text-center">
                {t(($) => $.join.title, { workspace: info.workspace_name })}
              </h1>
              {info.creator_name && (
                <p className="text-center text-muted-foreground">
                  {t(($) => $.join.invited_by, { name: info.creator_name })}
                </p>
              )}
              <p className="flex items-center justify-center gap-2 text-center text-body text-muted-foreground">
                <span>{t(($) => $.join.join_as)}</span>
                <Badge variant="outline">
                  {info.role === "admin" ? t(($) => $.join.role_admin) : t(($) => $.join.role_member)}
                </Badge>
              </p>
              {!user && (
                <p className="text-center text-body text-muted-foreground">
                  {t(($) => $.join.login_required)}
                </p>
              )}
              {joinError && (
                <p className="text-center text-body text-destructive">{joinError}</p>
              )}
              <div className="flex justify-center gap-2 pt-2">
                <Button onClick={handleJoin} disabled={joining}>
                  {joining
                    ? t(($) => $.join.joining)
                    : user
                      ? t(($) => $.join.join)
                      : t(($) => $.join.log_in_to_join)}
                </Button>
              </div>
            </>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

function JoinFallback() {
  const { t } = useT("invite");
  return <div className="flex min-h-screen items-center justify-center">{t(($) => $.join.loading)}</div>;
}

export default function JoinPage() {
  return (
    <Suspense fallback={<JoinFallback />}>
      <JoinInner />
    </Suspense>
  );
}
