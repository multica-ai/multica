"use client";

import { useEffect, useId, useRef, useState, type ReactNode } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useT } from "../../../../i18n";
import { PlanWriteNotice } from "./plan-write-notice";
import { usePlanWriteError } from "./use-plan-write-error";

export interface PlanTextFieldSpec {
  name: "title" | "description" | "acceptance_criteria";
  label: string;
  initial: string;
  multiline?: boolean;
  /** Rows for a multiline field. */
  rows?: number;
  placeholder?: string;
}

export type PlanFormValues = Partial<Record<PlanTextFieldSpec["name"], string>>;

/**
 * Shared shell for every plan authoring form (create plan, edit plan,
 * add/rename phase, add/edit part, supersede).
 *
 * All of them are the same interaction — a short set of text fields, a
 * server-validated submit, and an inline failure that keeps the dialog open —
 * so they share one implementation rather than six near-copies that drift.
 *
 * `title` is the only field the service requires
 * (`projectplan.validateTitle`), so submit is disabled on an empty one. That
 * is not client-side business logic duplicating the server: it is not letting
 * a person fire a request the service is certain to refuse. Everything else —
 * position conflicts, kind support, plan state — is decided server-side and
 * surfaced through `PlanWriteNotice`.
 */
export function PlanFormDialog({
  open,
  onOpenChange,
  heading,
  description,
  submitLabel,
  fields,
  extra,
  closeOnSubmit = true,
  onSubmit,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  heading: string;
  description: string;
  submitLabel: string;
  fields: PlanTextFieldSpec[];
  /** Extra static content shown above the actions (e.g. what supersede will do). */
  extra?: ReactNode;
  /**
   * False when `onSubmit` advances to another dialog stage rather than saving —
   * supersede's form stage does this. Closing afterwards would wipe the stage
   * `onSubmit` just opened, which silently swallowed the confirmation step.
   */
  closeOnSubmit?: boolean;
  onSubmit: (values: PlanFormValues) => Promise<void>;
}) {
  const { t } = useT("issues");
  const idPrefix = useId();
  const { notice, report, clear } = usePlanWriteError();
  const [values, setValues] = useState<PlanFormValues>({});
  const [submitting, setSubmitting] = useState(false);

  // `fields` is rebuilt on every render by the caller, so it cannot be an
  // effect dependency — naming it would reset the form on every keystroke.
  // Reading it through a ref keeps the reset once-per-open and keeps the
  // dependency array honest, with no lint suppression.
  const fieldsRef = useRef(fields);
  fieldsRef.current = fields;

  // Reset on each open so a cancelled edit never leaks into the next one.
  useEffect(() => {
    if (!open) return;
    setValues(Object.fromEntries(fieldsRef.current.map((f) => [f.name, f.initial])));
    setSubmitting(false);
    clear();
  }, [open, clear]);

  const titleField = fields.find((f) => f.name === "title");
  const titleMissing = !!titleField && !(values.title ?? "").trim();

  const handleOpenChange = (next: boolean) => {
    if (submitting) return;
    onOpenChange(next);
  };

  const handleSubmit = async () => {
    if (submitting || titleMissing) return;
    setSubmitting(true);
    clear();
    try {
      await onSubmit(values);
      if (closeOnSubmit) onOpenChange(false);
    } catch (err) {
      report(err);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="w-[calc(100vw-2rem)] !max-w-[520px]">
        <DialogHeader>
          <DialogTitle className="text-title-sm font-semibold">{heading}</DialogTitle>
          <DialogDescription className="text-body text-pretty">{description}</DialogDescription>
        </DialogHeader>

        <form
          className="flex flex-col gap-3.5"
          onSubmit={(e) => {
            e.preventDefault();
            void handleSubmit();
          }}
        >
          {fields.map((field) => {
            const id = `${idPrefix}-${field.name}`;
            const value = values[field.name] ?? "";
            return (
              <div key={field.name} className="flex flex-col gap-1.5">
                <Label htmlFor={id} className="text-caption font-medium">
                  {field.label}
                </Label>
                {field.multiline ? (
                  <Textarea
                    id={id}
                    rows={field.rows ?? 3}
                    value={value}
                    placeholder={field.placeholder}
                    onChange={(e) => setValues((v) => ({ ...v, [field.name]: e.target.value }))}
                  />
                ) : (
                  <Input
                    id={id}
                    value={value}
                    placeholder={field.placeholder}
                    // The first text field is the one a keyboard user lands on.
                    autoFocus={field === fields[0]}
                    onChange={(e) => setValues((v) => ({ ...v, [field.name]: e.target.value }))}
                  />
                )}
              </div>
            );
          })}

          {extra}
          <PlanWriteNotice notice={notice} />

          <DialogFooter className="mt-1">
            <Button
              type="button"
              variant="outline"
              onClick={() => handleOpenChange(false)}
              disabled={submitting}
            >
              {t(($) => $.plan.authoring.cancel)}
            </Button>
            <Button type="submit" disabled={submitting || titleMissing}>
              {submitting ? t(($) => $.plan.authoring.saving) : submitLabel}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
