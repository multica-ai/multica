"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { errorCode } from "@multica/core/api";
import { companySquadMembersOptions } from "@multica/core/company";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  initiativeActions,
  useAdvanceInitiative,
  useAttestInitiative,
  useCloseInitiative,
  useResumeInitiative,
  useSendBackInitiative,
  useShelveInitiative,
  type Initiative,
  type PipelineStage,
} from "@multica/core/pipeline";
import { useCurrentMember } from "@multica/core/permissions";
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useT } from "../i18n";

// The server's stable codes for a refused transition. Any other code, or none,
// shows the generic sentence instead of the server's English one.
const ACTION_ERROR_CODES = [
  "stage_not_ready",
  "needs_attest",
  "stage_is_gate",
  "not_attest_stage",
  "initiative_closed",
  "initiative_shelved",
  "initiative_not_shelved",
  "no_previous_stage",
  "note_required",
  "stage_unowned",
  "build_squad_not_sdlc",
] as const;

type ActionErrorKey = (typeof ACTION_ERROR_CODES)[number] | "generic";

function actionErrorKey(err: unknown): ActionErrorKey {
  const code = errorCode(err);
  return ACTION_ERROR_CODES.find((known) => known === code) ?? "generic";
}

/**
 * The stage transitions an owner, admin or owning squad member may take. Each
 * button shows only when the server would accept it (see initiativeActions), and
 * a refused change reads its code's sentence. Shelve and Close confirm first.
 */
export function InitiativeActions({
  initiative,
  stages,
  canManage,
}: {
  initiative: Initiative;
  stages: readonly PipelineStage[];
  canManage: boolean;
}) {
  const { t } = useT("pipeline");
  const wsId = useWorkspaceId();
  const { userId } = useCurrentMember(wsId);
  const members = useQuery(companySquadMembersOptions(wsId, initiative.owner_squad_id ?? ""));
  const squadMember =
    !!userId &&
    !!initiative.owner_squad_id &&
    (members.data ?? []).some((member) => member.member_type === "member" && member.member_id === userId);

  const stageIndex = stages.findIndex((stage) => stage.key === initiative.stage_key);
  const stage = stageIndex >= 0 ? stages[stageIndex] : undefined;
  const allowed = initiativeActions({
    admin: canManage,
    squadMember,
    exitMode: stage?.exit.mode ?? null,
    status: initiative.status,
    hasPreviousStage: stageIndex > 0,
  });

  const advance = useAdvanceInitiative(wsId);
  const attest = useAttestInitiative(wsId);
  const resume = useResumeInitiative(wsId);
  const sendBack = useSendBackInitiative(wsId);
  const [sendingBack, setSendingBack] = useState(false);
  const [note, setNote] = useState("");
  const [noteMissing, setNoteMissing] = useState(false);
  const [confirming, setConfirming] = useState<"shelve" | "close" | null>(null);

  if (!Object.values(allowed).some(Boolean)) return null;

  const id = initiative.id;
  const pending = advance.isPending || attest.isPending || resume.isPending || sendBack.isPending;
  const failed = (err: Error) => toast.error(t(($) => $.action_error[actionErrorKey(err)]));

  const submitSendBack = () => {
    const trimmed = note.trim();
    if (!trimmed) {
      setNoteMissing(true);
      return;
    }
    sendBack.mutate(
      { id, note: trimmed },
      {
        onSuccess: () => {
          toast.success(t(($) => $.toast.sent_back));
          setSendingBack(false);
          setNote("");
          setNoteMissing(false);
        },
        onError: failed,
      },
    );
  };

  return (
    <section aria-labelledby="initiative-actions" className="flex flex-col gap-3">
      <h3 id="initiative-actions" className="text-caption font-medium text-muted-foreground">
        {t(($) => $.panel.actions)}
      </h3>
      {!sendingBack ? (
        <div className="flex flex-wrap gap-2">
          {allowed.attest ? (
            <Button
              type="button"
              size="sm"
              disabled={pending}
              aria-busy={attest.isPending}
              onClick={() =>
                attest.mutate(
                  { id },
                  { onSuccess: () => toast.success(t(($) => $.toast.attested)), onError: failed },
                )
              }
            >
              {t(($) => $.actions.attest)}
            </Button>
          ) : null}
          {allowed.advance ? (
            <Button
              type="button"
              size="sm"
              variant={allowed.attest ? "outline" : "default"}
              disabled={pending}
              aria-busy={advance.isPending}
              onClick={() =>
                advance.mutate(
                  { id },
                  { onSuccess: () => toast.success(t(($) => $.toast.advanced)), onError: failed },
                )
              }
            >
              {t(($) => $.actions.advance)}
            </Button>
          ) : null}
          {allowed.send_back ? (
            <Button type="button" variant="outline" size="sm" disabled={pending} onClick={() => setSendingBack(true)}>
              {t(($) => $.actions.send_back)}
            </Button>
          ) : null}
          {allowed.resume ? (
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={pending}
              aria-busy={resume.isPending}
              onClick={() =>
                resume.mutate(
                  { id },
                  { onSuccess: () => toast.success(t(($) => $.toast.resumed)), onError: failed },
                )
              }
            >
              {t(($) => $.actions.resume)}
            </Button>
          ) : null}
          {allowed.shelve ? (
            <Button type="button" variant="outline" size="sm" disabled={pending} onClick={() => setConfirming("shelve")}>
              {t(($) => $.actions.shelve)}
            </Button>
          ) : null}
          {allowed.close ? (
            <Button type="button" variant="outline" size="sm" disabled={pending} onClick={() => setConfirming("close")}>
              {t(($) => $.actions.close)}
            </Button>
          ) : null}
        </div>
      ) : (
        <div className="flex flex-col gap-2">
          <label className="flex flex-col gap-1 text-caption text-muted-foreground">
            {t(($) => $.actions.send_back_note)}
            <Textarea
              value={note}
              rows={2}
              aria-invalid={noteMissing || undefined}
              onChange={(event) => {
                setNote(event.target.value);
                if (event.target.value.trim()) setNoteMissing(false);
              }}
              className="text-body text-foreground"
            />
          </label>
          {noteMissing ? (
            <p role="alert" className="text-caption text-destructive">
              {t(($) => $.actions.send_back_note_required)}
            </p>
          ) : null}
          <div className="flex justify-end gap-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => {
                setSendingBack(false);
                setNote("");
                setNoteMissing(false);
              }}
            >
              {t(($) => $.actions.cancel)}
            </Button>
            <Button type="button" size="sm" disabled={sendBack.isPending} aria-busy={sendBack.isPending} onClick={submitSendBack}>
              {t(($) => $.actions.send_back_submit)}
            </Button>
          </div>
        </div>
      )}

      <AlertDialog open={confirming === "shelve"} onOpenChange={(open) => !open && setConfirming(null)}>
        <AlertDialogContent>
          <ShelveConfirm initiativeId={id} onDone={() => setConfirming(null)} />
        </AlertDialogContent>
      </AlertDialog>
      <AlertDialog open={confirming === "close"} onOpenChange={(open) => !open && setConfirming(null)}>
        <AlertDialogContent>
          <CloseConfirm initiativeId={id} onDone={() => setConfirming(null)} />
        </AlertDialogContent>
      </AlertDialog>
    </section>
  );
}

// The confirmations own their note or reason, so it starts empty each time the
// dialog opens. The dialog closes only once the server has accepted the change.

function ShelveConfirm({ initiativeId, onDone }: { initiativeId: string; onDone: () => void }) {
  const { t } = useT("pipeline");
  const wsId = useWorkspaceId();
  const shelve = useShelveInitiative(wsId);
  const [note, setNote] = useState("");

  return (
    <>
      <AlertDialogHeader>
        <AlertDialogTitle>{t(($) => $.actions.shelve_title)}</AlertDialogTitle>
        <AlertDialogDescription>{t(($) => $.actions.shelve_description)}</AlertDialogDescription>
      </AlertDialogHeader>
      <label className="flex flex-col gap-1 text-caption text-muted-foreground">
        {t(($) => $.actions.shelve_note)}
        <Textarea value={note} rows={2} onChange={(event) => setNote(event.target.value)} className="text-body text-foreground" />
      </label>
      <AlertDialogFooter>
        <AlertDialogCancel>{t(($) => $.actions.cancel)}</AlertDialogCancel>
        <Button
          type="button"
          disabled={shelve.isPending}
          aria-busy={shelve.isPending}
          onClick={() =>
            shelve.mutate(
              { id: initiativeId, note: note.trim() },
              {
                onSuccess: () => {
                  toast.success(t(($) => $.toast.shelved));
                  onDone();
                },
                onError: (err) => toast.error(t(($) => $.action_error[actionErrorKey(err)])),
              },
            )
          }
        >
          {t(($) => $.actions.shelve_submit)}
        </Button>
      </AlertDialogFooter>
    </>
  );
}

function CloseConfirm({ initiativeId, onDone }: { initiativeId: string; onDone: () => void }) {
  const { t } = useT("pipeline");
  const wsId = useWorkspaceId();
  const close = useCloseInitiative(wsId);
  const [reason, setReason] = useState("");
  const [reasonMissing, setReasonMissing] = useState(false);

  const submit = () => {
    const trimmed = reason.trim();
    if (!trimmed) {
      setReasonMissing(true);
      return;
    }
    close.mutate(
      { id: initiativeId, reason: trimmed },
      {
        onSuccess: () => {
          toast.success(t(($) => $.toast.closed));
          onDone();
        },
        onError: (err) => toast.error(t(($) => $.action_error[actionErrorKey(err)])),
      },
    );
  };

  return (
    <>
      <AlertDialogHeader>
        <AlertDialogTitle>{t(($) => $.actions.close_title)}</AlertDialogTitle>
        <AlertDialogDescription>{t(($) => $.actions.close_description)}</AlertDialogDescription>
      </AlertDialogHeader>
      <label className="flex flex-col gap-1 text-caption text-muted-foreground">
        {t(($) => $.actions.close_reason)}
        <Textarea
          value={reason}
          rows={2}
          aria-invalid={reasonMissing || undefined}
          onChange={(event) => {
            setReason(event.target.value);
            if (event.target.value.trim()) setReasonMissing(false);
          }}
          className="text-body text-foreground"
        />
      </label>
      {reasonMissing ? (
        <p role="alert" className="text-caption text-destructive">
          {t(($) => $.actions.close_reason_required)}
        </p>
      ) : null}
      <AlertDialogFooter>
        <AlertDialogCancel>{t(($) => $.actions.cancel)}</AlertDialogCancel>
        <Button type="button" variant="destructive" disabled={close.isPending} aria-busy={close.isPending} onClick={submit}>
          {t(($) => $.actions.close_submit)}
        </Button>
      </AlertDialogFooter>
    </>
  );
}
