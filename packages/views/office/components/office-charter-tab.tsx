"use client";

import { useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { ApiError, clientErrorMessage } from "@multica/core/api";
import {
  CHARTER_CADENCE_HINT_MAX,
  CHARTER_ITEM_TEXT_MAX,
  CHARTER_KB_KEY_MAX,
  CHARTER_LIST_MAX,
  CHARTER_OUTPUT_KINDS,
  CHARTER_RESPONSE_HOURS_MAX,
  CHARTER_RESPONSE_HOURS_MIN,
  CHARTER_TEXT_MAX,
  parseCharterRevisionConflict,
  squadCharterOptions,
  useSaveSquadCharter,
  type CharterOutputKind,
  type SquadCharter,
  type SquadCharterOutputInput,
  type UpdateSquadCharterRequest,
} from "@multica/core/office";
import { useWorkspaceId } from "@multica/core/hooks";
import { Alert, AlertDescription } from "@multica/ui/components/ui/alert";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Switch } from "@multica/ui/components/ui/switch";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Loader2 } from "@multica/ui/icons";
import { useT } from "../../i18n";

type Draft = {
  mission: string;
  responsibilities: { key: string; text: string }[];
  outputs: { key: string; kind: CharterOutputKind; kb_key: string; cadence_hint: string }[];
  boundaries: string;
  escalation: string;
  intake: string;
  responseHours: string;
  operatingReview: boolean;
};

/**
 * A squad's charter: read for every member, edited by an admin or the squad's
 * creator. The editor saves at the revision it opened; a save against a newer
 * charter is refused with a conflict, and the editor offers a reload.
 */
export function OfficeCharterTab({ squadId, canEdit }: { squadId: string; canEdit: boolean }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const query = useQuery(squadCharterOptions(wsId, squadId));
  const [editing, setEditing] = useState(false);
  // Bumped on reload, so the editor restarts from the server's charter.
  const [formVersion, setFormVersion] = useState(0);

  if (query.isError) {
    return <p className="text-body text-muted-foreground">{t(($) => $.office.charter_load_failed)}</p>;
  }
  if (!query.data) {
    return <p className="text-body text-muted-foreground">{t(($) => $.office.charter_loading)}</p>;
  }
  const charter = query.data;

  // A squad with no charter opens straight into the editor. The editor is keyed by
  // formVersion only, so a background refetch never drops an edit in progress.
  if (canEdit && (editing || charter.revision === 0)) {
    return (
      <CharterForm
        key={formVersion}
        charter={charter}
        canCancel={charter.revision > 0}
        onCancel={() => setEditing(false)}
        onSaved={() => setEditing(false)}
        onReload={async () => {
          await query.refetch();
          setFormVersion((version) => version + 1);
        }}
        squadId={squadId}
        wsId={wsId}
      />
    );
  }
  return <CharterRead charter={charter} canEdit={canEdit} onEdit={() => setEditing(true)} />;
}

function CharterRead({
  charter,
  canEdit,
  onEdit,
}: {
  charter: SquadCharter;
  canEdit: boolean;
  onEdit: () => void;
}) {
  const { t } = useT("issues");
  const kindLabel = useCharterKindLabels();
  const notSet = t(($) => $.office.charter_not_set);

  if (charter.revision === 0) {
    return <p className="text-body text-muted-foreground">{t(($) => $.office.charter_none_yet)}</p>;
  }

  const textOrNotSet = (value: string) =>
    value ? (
      <p className="whitespace-pre-line text-body">{value}</p>
    ) : (
      <p className="text-body text-muted-foreground">{notSet}</p>
    );

  return (
    <div className="flex flex-col gap-4">
      {canEdit ? (
        <div className="flex justify-end">
          <Button type="button" variant="outline" size="sm" onClick={onEdit}>
            {t(($) => $.office.charter_edit)}
          </Button>
        </div>
      ) : null}
      <ReadSection title={t(($) => $.office.charter_mission)}>{textOrNotSet(charter.mission)}</ReadSection>
      <ReadSection title={t(($) => $.office.charter_responsibilities)}>
        {charter.responsibilities.length === 0 ? (
          textOrNotSet("")
        ) : (
          <ul className="flex list-disc flex-col gap-1 pl-5 text-body">
            {charter.responsibilities.map((item) => (
              <li key={item.key}>{item.text}</li>
            ))}
          </ul>
        )}
      </ReadSection>
      <ReadSection title={t(($) => $.office.charter_outputs)}>
        {charter.outputs.length === 0 ? (
          textOrNotSet("")
        ) : (
          <ul className="flex flex-col gap-1 text-body">
            {charter.outputs.map((output) => (
              <li key={output.key}>
                {[kindLabel[output.kind], output.kb_key, output.cadence_hint].filter(Boolean).join(" · ")}
              </li>
            ))}
          </ul>
        )}
      </ReadSection>
      <ReadSection title={t(($) => $.office.charter_boundaries)}>{textOrNotSet(charter.boundaries)}</ReadSection>
      <ReadSection title={t(($) => $.office.charter_escalation)}>{textOrNotSet(charter.escalation)}</ReadSection>
      <ReadSection title={t(($) => $.office.charter_intake)}>{textOrNotSet(charter.intake)}</ReadSection>
      <ReadSection title={t(($) => $.office.charter_response_time)}>
        <p className="text-body">{t(($) => $.office.charter_hours_read, { count: charter.response_hours })}</p>
      </ReadSection>
      <ReadSection title={t(($) => $.office.charter_review)}>
        <p className="text-body">
          {charter.operating_review_enabled
            ? t(($) => $.office.charter_review_on)
            : t(($) => $.office.charter_review_off)}
        </p>
      </ReadSection>
    </div>
  );
}

function ReadSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-1">
      <h3 className="text-caption font-medium text-muted-foreground">{title}</h3>
      {children}
    </section>
  );
}

function CharterForm({
  squadId,
  wsId,
  charter,
  canCancel,
  onCancel,
  onSaved,
  onReload,
}: {
  squadId: string;
  wsId: string;
  charter: SquadCharter;
  canCancel: boolean;
  onCancel: () => void;
  onSaved: () => void;
  onReload: () => Promise<void>;
}) {
  const { t } = useT("issues");
  const { t: tCommon } = useT("common");
  const kindLabel = useCharterKindLabels();
  const save = useSaveSquadCharter(wsId, squadId);
  // The revision the editor opened. A save at any other revision is a conflict.
  const [baseRevision] = useState(charter.revision);
  const [draft, setDraft] = useState<Draft>(() => draftFrom(charter));
  const [conflict, setConflict] = useState(false);

  const hours = parseHours(draft.responseHours);
  const hasUnknownKind = draft.outputs.some((output) => output.kind === "unknown");
  const canSubmit = hours !== null && !hasUnknownKind && !save.isPending;

  const update = (patch: Partial<Draft>) => setDraft((current) => ({ ...current, ...patch }));

  const submit = () => {
    if (hours === null) return;
    // Only kinds the server accepts can be sent; the save button is disabled
    // while a row still holds an unknown kind.
    const outputs: SquadCharterOutputInput[] = [];
    for (const output of draft.outputs) {
      if (output.kind === "unknown") return;
      outputs.push({
        key: output.key,
        kind: output.kind,
        kb_key: output.kb_key.trim(),
        cadence_hint: output.cadence_hint.trim(),
      });
    }
    const request: UpdateSquadCharterRequest = {
      revision: baseRevision,
      mission: draft.mission.trim(),
      responsibilities: draft.responsibilities
        .map((item) => ({ key: item.key, text: item.text.trim() }))
        .filter((item) => item.text !== ""),
      outputs,
      boundaries: draft.boundaries.trim(),
      escalation: draft.escalation.trim(),
      intake: draft.intake.trim(),
      response_hours: hours,
      operating_review_enabled: draft.operatingReview,
    };
    save.mutate(request, {
      onSuccess: () => {
        toast.success(t(($) => $.office.charter_saved));
        onSaved();
      },
      onError: (err) => {
        const current = err instanceof ApiError ? parseCharterRevisionConflict(err.body) : null;
        if (current !== null) {
          setConflict(true);
          return;
        }
        toast.error(clientErrorMessage(err) ?? t(($) => $.office.charter_save_failed));
      },
    });
  };

  const kindItems = (current: CharterOutputKind) => [
    ...CHARTER_OUTPUT_KINDS.map((kind) => ({ value: kind, label: kindLabel[kind] })),
    ...(current === "unknown" ? [{ value: "unknown", label: kindLabel.unknown }] : []),
  ];

  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        submit();
      }}
    >
      <Field id="charter-mission" label={t(($) => $.office.charter_mission)}>
        <Textarea
          id="charter-mission"
          rows={3}
          maxLength={CHARTER_TEXT_MAX}
          placeholder={t(($) => $.office.charter_mission_placeholder)}
          value={draft.mission}
          onChange={(event) => update({ mission: event.target.value })}
        />
      </Field>

      <div className="flex flex-col gap-1.5">
        <span className="text-caption font-medium text-muted-foreground">
          {t(($) => $.office.charter_responsibilities)}
        </span>
        <ul className="flex flex-col gap-2">
          {draft.responsibilities.map((item, index) => (
            <li key={item.key} className="flex items-center gap-2">
              <Input
                aria-label={t(($) => $.office.charter_responsibilities)}
                maxLength={CHARTER_ITEM_TEXT_MAX}
                placeholder={t(($) => $.office.charter_responsibility_placeholder)}
                value={item.text}
                onChange={(event) =>
                  update({
                    responsibilities: draft.responsibilities.map((row, i) =>
                      i === index ? { ...row, text: event.target.value } : row,
                    ),
                  })
                }
              />
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={() =>
                  update({ responsibilities: draft.responsibilities.filter((_, i) => i !== index) })
                }
              >
                {t(($) => $.office.charter_remove_item)}
              </Button>
            </li>
          ))}
        </ul>
        <div>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={draft.responsibilities.length >= CHARTER_LIST_MAX}
            onClick={() =>
              update({
                responsibilities: [
                  ...draft.responsibilities,
                  { key: freshKey("r", draft.responsibilities.map((r) => r.key)), text: "" },
                ],
              })
            }
          >
            {t(($) => $.office.charter_add_responsibility)}
          </Button>
        </div>
      </div>

      <div className="flex flex-col gap-1.5">
        <span className="text-caption font-medium text-muted-foreground">{t(($) => $.office.charter_outputs)}</span>
        <ul className="flex flex-col gap-2">
          {draft.outputs.map((output, index) => (
            <li key={output.key} className="flex flex-col gap-2 rounded-md border p-2">
              <div className="flex items-center gap-2">
                <Select
                  items={kindItems(output.kind)}
                  value={output.kind}
                  onValueChange={(next) => {
                    if (!next) return;
                    update({
                      outputs: draft.outputs.map((row, i) =>
                        i === index ? { ...row, kind: next as CharterOutputKind } : row,
                      ),
                    });
                  }}
                >
                  <SelectTrigger
                    size="sm"
                    className="w-40 shrink-0"
                    aria-label={t(($) => $.office.charter_output_kind)}
                  >
                    <SelectValue>{kindLabel[output.kind]}</SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    {kindItems(output.kind).map((item) => (
                      <SelectItem key={item.value} value={item.value}>
                        {item.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <Input
                  aria-label={t(($) => $.office.charter_output_kb)}
                  maxLength={CHARTER_KB_KEY_MAX}
                  placeholder={t(($) => $.office.charter_output_kb_placeholder)}
                  value={output.kb_key}
                  onChange={(event) =>
                    update({
                      outputs: draft.outputs.map((row, i) =>
                        i === index ? { ...row, kb_key: event.target.value } : row,
                      ),
                    })
                  }
                />
              </div>
              <div className="flex items-center gap-2">
                <Input
                  aria-label={t(($) => $.office.charter_output_cadence)}
                  maxLength={CHARTER_CADENCE_HINT_MAX}
                  placeholder={t(($) => $.office.charter_output_cadence_placeholder)}
                  value={output.cadence_hint}
                  onChange={(event) =>
                    update({
                      outputs: draft.outputs.map((row, i) =>
                        i === index ? { ...row, cadence_hint: event.target.value } : row,
                      ),
                    })
                  }
                />
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => update({ outputs: draft.outputs.filter((_, i) => i !== index) })}
                >
                  {t(($) => $.office.charter_remove_item)}
                </Button>
              </div>
            </li>
          ))}
        </ul>
        <div>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={draft.outputs.length >= CHARTER_LIST_MAX}
            onClick={() =>
              update({
                outputs: [
                  ...draft.outputs,
                  { key: freshKey("o", draft.outputs.map((o) => o.key)), kind: "report", kb_key: "", cadence_hint: "" },
                ],
              })
            }
          >
            {t(($) => $.office.charter_add_output)}
          </Button>
        </div>
      </div>

      <Field id="charter-boundaries" label={t(($) => $.office.charter_boundaries)}>
        <Textarea
          id="charter-boundaries"
          rows={2}
          maxLength={CHARTER_TEXT_MAX}
          placeholder={t(($) => $.office.charter_boundaries_placeholder)}
          value={draft.boundaries}
          onChange={(event) => update({ boundaries: event.target.value })}
        />
      </Field>
      <Field id="charter-escalation" label={t(($) => $.office.charter_escalation)}>
        <Textarea
          id="charter-escalation"
          rows={2}
          maxLength={CHARTER_TEXT_MAX}
          placeholder={t(($) => $.office.charter_escalation_placeholder)}
          value={draft.escalation}
          onChange={(event) => update({ escalation: event.target.value })}
        />
      </Field>
      <Field id="charter-intake" label={t(($) => $.office.charter_intake)}>
        <Textarea
          id="charter-intake"
          rows={2}
          maxLength={CHARTER_TEXT_MAX}
          placeholder={t(($) => $.office.charter_intake_placeholder)}
          value={draft.intake}
          onChange={(event) => update({ intake: event.target.value })}
        />
      </Field>

      <Field id="charter-hours" label={t(($) => $.office.charter_hours_label)}>
        <Input
          id="charter-hours"
          type="number"
          inputMode="numeric"
          min={CHARTER_RESPONSE_HOURS_MIN}
          max={CHARTER_RESPONSE_HOURS_MAX}
          className="w-32"
          aria-invalid={hours === null}
          value={draft.responseHours}
          onChange={(event) => update({ responseHours: event.target.value })}
        />
        {hours === null ? (
          <p className="text-caption text-destructive">{t(($) => $.office.charter_hours_error)}</p>
        ) : null}
      </Field>

      <label className="flex items-start justify-between gap-3">
        <span className="flex min-w-0 flex-col gap-0.5">
          <span className="text-body">{t(($) => $.office.charter_review)}</span>
          <span className="text-caption text-muted-foreground">{t(($) => $.office.charter_review_hint)}</span>
        </span>
        <Switch
          checked={draft.operatingReview}
          onCheckedChange={(checked) => update({ operatingReview: checked })}
        />
      </label>

      {hasUnknownKind ? (
        <p className="text-caption text-destructive">{t(($) => $.office.charter_kind_required)}</p>
      ) : null}

      {conflict ? (
        <Alert variant="destructive">
          <AlertDescription className="flex flex-wrap items-center justify-between gap-2">
            <span>{t(($) => $.office.charter_conflict)}</span>
            <Button type="button" variant="outline" size="sm" onClick={() => void onReload()}>
              {t(($) => $.office.charter_reload)}
            </Button>
          </AlertDescription>
        </Alert>
      ) : null}

      <div className="flex justify-end gap-2">
        {canCancel ? (
          <Button type="button" variant="outline" onClick={onCancel}>
            {tCommon(($) => $.cancel)}
          </Button>
        ) : null}
        <Button type="submit" disabled={!canSubmit} aria-busy={save.isPending}>
          {save.isPending ? <Loader2 className="animate-spin" /> : null}
          {save.isPending ? t(($) => $.office.charter_saving) : t(($) => $.office.charter_save)}
        </Button>
      </div>
    </form>
  );
}

function Field({ id, label, children }: { id: string; label: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1.5">
      <Label htmlFor={id} className="text-caption font-medium text-muted-foreground">
        {label}
      </Label>
      {children}
    </div>
  );
}

export function useCharterKindLabels(): Record<CharterOutputKind, string> {
  const { t } = useT("issues");
  return {
    issue: t(($) => $.office.charter_kind_issue),
    proposal: t(($) => $.office.charter_kind_proposal),
    kb_document: t(($) => $.office.charter_kind_kb_document),
    report: t(($) => $.office.charter_kind_report),
    unknown: t(($) => $.office.charter_kind_unknown),
  };
}

function draftFrom(charter: SquadCharter): Draft {
  return {
    mission: charter.mission,
    responsibilities: charter.responsibilities.map((item) => ({ key: item.key, text: item.text })),
    outputs: charter.outputs.map((output) => ({
      key: output.key,
      kind: output.kind,
      kb_key: output.kb_key,
      cadence_hint: output.cadence_hint,
    })),
    boundaries: charter.boundaries,
    escalation: charter.escalation,
    intake: charter.intake,
    responseHours: String(charter.response_hours),
    operatingReview: charter.operating_review_enabled,
  };
}

/** A whole number of hours in the server's range, or null. */
function parseHours(value: string): number | null {
  const n = Number(value.trim());
  return Number.isInteger(n) && n >= CHARTER_RESPONSE_HOURS_MIN && n <= CHARTER_RESPONSE_HOURS_MAX ? n : null;
}

/** A list key unused in the list, so a new row never collides with a saved one. */
function freshKey(prefix: "r" | "o", taken: string[]): string {
  const used = new Set(taken);
  for (let n = 1; ; n += 1) {
    const key = `${prefix}${n}`;
    if (!used.has(key)) return key;
  }
}
