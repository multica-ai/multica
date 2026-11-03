"use client";

import { useState } from "react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWaiveInitiativeArtifact, type InitiativeArtifact } from "@multica/core/pipeline";
import { useWorkspacePaths } from "@multica/core/paths";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useT } from "../i18n";
import { AppLink } from "../navigation";
import { humanizeKey } from "./key-label";

/**
 * The artifacts the current stage asks for. A published one links to the
 * knowledge base. An owner or admin may waive a missing required artifact, and
 * the waiver needs a note, which the server keeps with it.
 */
export function InitiativeArtifacts({
  initiativeId,
  artifacts,
  canManage,
}: {
  initiativeId: string;
  artifacts: readonly InitiativeArtifact[];
  canManage: boolean;
}) {
  const { t } = useT("pipeline");
  return (
    <section aria-labelledby="initiative-artifacts" className="flex flex-col gap-2">
      <h3 id="initiative-artifacts" className="text-caption font-medium text-muted-foreground">
        {t(($) => $.panel.artifacts)}
      </h3>
      {artifacts.length === 0 ? (
        <p className="text-caption text-muted-foreground">{t(($) => $.artifact.none)}</p>
      ) : (
        <ul className="flex flex-col divide-y rounded-lg border">
          {artifacts.map((artifact) => (
            <ArtifactRow
              key={artifact.kind}
              initiativeId={initiativeId}
              artifact={artifact}
              canManage={canManage}
            />
          ))}
        </ul>
      )}
    </section>
  );
}

function ArtifactRow({
  initiativeId,
  artifact,
  canManage,
}: {
  initiativeId: string;
  artifact: InitiativeArtifact;
  canManage: boolean;
}) {
  const { t } = useT("pipeline");
  const paths = useWorkspacePaths();
  const wsId = useWorkspaceId();
  const waive = useWaiveInitiativeArtifact(wsId);
  const [waiving, setWaiving] = useState(false);
  const [note, setNote] = useState("");
  const [noteMissing, setNoteMissing] = useState(false);

  const present = artifact.knowledge_link_id !== null;
  const canWaive = canManage && artifact.required && !present && !artifact.waived;

  const submitWaiver = () => {
    const trimmed = note.trim();
    if (!trimmed) {
      setNoteMissing(true);
      return;
    }
    waive.mutate(
      { id: initiativeId, kind: artifact.kind, note: trimmed },
      {
        onSuccess: () => {
          toast.success(t(($) => $.toast.waived));
          setWaiving(false);
          setNote("");
        },
        onError: () => toast.error(t(($) => $.toast.waive_failed)),
      },
    );
  };

  return (
    <li className="flex flex-col gap-2 px-3 py-2.5">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span className="min-w-0 text-body">{humanizeKey(artifact.kind)}</span>
        <Badge variant="outline">
          {artifact.required ? t(($) => $.artifact.required) : t(($) => $.artifact.optional)}
        </Badge>
      </div>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-caption text-muted-foreground">
        {present ? (
          <>
            <span>{t(($) => $.artifact.published)}</span>
            <AppLink href={paths.knowledge()} className="text-primary hover:underline">
              {t(($) => $.artifact.open_knowledge)}
            </AppLink>
          </>
        ) : artifact.waived ? (
          <span>{t(($) => $.artifact.waived)}</span>
        ) : (
          <span>{t(($) => $.artifact.missing)}</span>
        )}
        {canWaive && !waiving ? (
          <Button type="button" variant="ghost" size="xs" onClick={() => setWaiving(true)}>
            {t(($) => $.artifact.waive)}
          </Button>
        ) : null}
      </div>
      {artifact.waived && artifact.waived_note ? (
        <p className="whitespace-pre-line text-caption text-muted-foreground">{artifact.waived_note}</p>
      ) : null}
      {waiving ? (
        <div className="flex flex-col gap-2">
          <label className="flex flex-col gap-1 text-caption text-muted-foreground">
            {t(($) => $.artifact.waive_note)}
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
              {t(($) => $.artifact.waive_note_required)}
            </p>
          ) : null}
          <div className="flex justify-end gap-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => {
                setWaiving(false);
                setNote("");
                setNoteMissing(false);
              }}
            >
              {t(($) => $.gate.cancel)}
            </Button>
            <Button
              type="button"
              size="sm"
              disabled={waive.isPending}
              aria-busy={waive.isPending}
              onClick={submitWaiver}
            >
              {t(($) => $.artifact.waive_submit)}
            </Button>
          </div>
        </div>
      ) : null}
    </li>
  );
}
