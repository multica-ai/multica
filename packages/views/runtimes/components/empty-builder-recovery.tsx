"use client";

import { useState } from "react";
import { ApiError } from "@multica/core/api";
import {
  type EmptyAgentBuilderSession,
  useEmptyAgentBuilderSessions,
  useDiscardEmptyAgentBuilderSession,
} from "@multica/core/runtimes";
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";

export function EmptyBuilderRecovery({
  wsId,
  profileId,
  initiallyEnabled = false,
}: {
  wsId: string;
  profileId: string;
  initiallyEnabled?: boolean;
}) {
  const { t } = useT("runtimes");
  const [enabled, setEnabled] = useState(initiallyEnabled);
  const [selected, setSelected] = useState<EmptyAgentBuilderSession | null>(
    null,
  );
  const [changed, setChanged] = useState(false);
  const sessions = useEmptyAgentBuilderSessions(wsId, profileId, enabled);
  const discard = useDiscardEmptyAgentBuilderSession(wsId, profileId);
  const rows = sessions.data?.pages.flatMap((page) => page.sessions) ?? [];
  const error = discard.error ?? sessions.error;
  const errorMessage =
    error instanceof ApiError && error.status === 404
      ? t(($) => $.profiles.recovery.unavailable)
      : error instanceof ApiError && error.status === 403
        ? t(($) => $.profiles.recovery.forbidden)
        : error
          ? t(($) => $.profiles.recovery.error)
          : null;

  const confirm = async () => {
    if (!selected) return;
    setChanged(false);
    try {
      await discard.mutateAsync(selected);
      setSelected(null);
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        setSelected(null);
        discard.reset();
        setChanged(true);
        await sessions.refetch();
      }
    }
  };

  return (
    <section
      className="mt-3 space-y-2 text-caption"
      aria-label={t(($) => $.profiles.recovery.check)}
    >
      <Button
        type="button"
        variant="outline"
        size="sm"
        disabled={sessions.isFetching || discard.isPending}
        onClick={() => {
          discard.reset();
          setChanged(false);
          if (enabled) void sessions.refetch();
          else setEnabled(true);
        }}
      >
        {sessions.isFetching
          ? t(($) => $.profiles.recovery.loading)
          : t(($) => $.profiles.recovery.check)}
      </Button>
      {enabled && (
        <>
          <p className="text-muted-foreground">
            {t(($) => $.profiles.recovery.scope)}
          </p>
          {changed && (
            <p role="status">{t(($) => $.profiles.recovery.changed)}</p>
          )}
          {errorMessage && <p role="alert">{errorMessage}</p>}
          {sessions.isSuccess && !sessions.isFetching && rows.length === 0 && (
            <p>{t(($) => $.profiles.recovery.empty)}</p>
          )}
          {rows.length > 0 && (
            <ul className="max-h-56 space-y-2 overflow-y-auto">
              {rows.map((row) => (
                <li
                  key={row.sessionId}
                  className="flex items-center justify-between gap-2 rounded-md border p-2"
                >
                  <div className="min-w-0 break-words">
                    <p>
                      {row.machineLabel ||
                        t(($) => $.profiles.recovery.unknown_machine)}
                    </p>
                    <time dateTime={row.createdAt}>
                      {new Date(row.createdAt).toLocaleString()}
                    </time>
                  </div>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={
                      discard.isPending ||
                      sessions.isFetching ||
                      sessions.isError
                    }
                    onClick={() => {
                      discard.reset();
                      setChanged(false);
                      setSelected(row);
                    }}
                  >
                    {t(($) => $.profiles.recovery.discard)}
                  </Button>
                </li>
              ))}
            </ul>
          )}
          {sessions.hasNextPage && (
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={sessions.isFetching || discard.isPending}
              onClick={() => void sessions.fetchNextPage()}
            >
              {t(($) => $.profiles.recovery.more)}
            </Button>
          )}
        </>
      )}
      <AlertDialog
        open={selected !== null}
        onOpenChange={(open) => {
          if (!open && !discard.isPending) {
            setSelected(null);
            discard.reset();
          }
        }}
      >
        <AlertDialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto">
          <AlertDialogTitle>
            {t(($) => $.profiles.recovery.title)}
          </AlertDialogTitle>
          <AlertDialogDescription className="break-words">
            {t(($) => $.profiles.recovery.description, {
              machine:
                selected?.machineLabel ||
                t(($) => $.profiles.recovery.unknown_machine),
              created: selected
                ? new Date(selected.createdAt).toLocaleString()
                : "",
            })}
          </AlertDialogDescription>
          {errorMessage && (
            <p role="alert" className="text-caption">
              {errorMessage}
            </p>
          )}
          <div className="flex justify-end gap-2">
            <Button
              type="button"
              variant="outline"
              disabled={discard.isPending}
              onClick={() => {
                setSelected(null);
                discard.reset();
              }}
            >
              {t(($) => $.profiles.recovery.cancel)}
            </Button>
            <Button
              type="button"
              variant="destructive"
              disabled={discard.isPending}
              aria-busy={discard.isPending}
              onClick={() => void confirm()}
            >
              {discard.isPending
                ? t(($) => $.profiles.recovery.discarding)
                : t(($) => $.profiles.recovery.confirm)}
            </Button>
          </div>
        </AlertDialogContent>
      </AlertDialog>
    </section>
  );
}
