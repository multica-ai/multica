"use client";

import { memo, useState } from "react";
import { CheckCircle2, XCircle } from "lucide-react";
import { toast } from "sonner";
import { RadioGroup, RadioGroupItem } from "@multica/ui/components/ui/radio-group";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useActorName } from "@multica/core/workspace/hooks";
import { useAnswerAskUserQuestion } from "@multica/core/issues/mutations";
import type { AskUserQuestionMeta } from "@multica/core/types";
import { useT } from "../../i18n";

interface AskUserQuestionCardProps {
  issueId: string;
  commentId: string;
  meta: AskUserQuestionMeta;
  /** user_id of the currently signed-in member (undefined for agents). */
  currentUserId?: string;
  className?: string;
}

/**
 * Renders an ask_user_question comment as the fixed four-layer card:
 *   1. @target_user prompt line
 *   2. the question text
 *   3. an option list — radio (single) or checkbox (multi_select); when
 *      allow_custom is on, an auto "Other" row reveals a free-text input
 *   4. Confirm / Ignore buttons (or a terminal "Selected" / "Ignored" chip)
 *
 * Only the target_user may act; everyone else sees a read-only view. Once the
 * answer is recorded (via the answer endpoint → comment:updated WS event), the
 * card re-renders in its terminal state: options greyed, chosen ones ticked,
 * custom text shown, and the buttons replaced by a disabled chip.
 */
function AskUserQuestionCardImpl({
  issueId,
  commentId,
  meta,
  currentUserId,
  className,
}: AskUserQuestionCardProps) {
  const { t } = useT("issues");
  const { getActorName } = useActorName();
  const answer = useAnswerAskUserQuestion(issueId);

  const options = meta.options ?? [];
  const multi = meta.multi_select === true;
  const allowCustom = meta.allow_custom === true;
  const existingAnswer = meta.answer ?? null;
  const isAnswered = existingAnswer != null;
  const isSubmitted = existingAnswer?.state === "submitted";
  const isIgnored = existingAnswer?.state === "ignored";

  // Only the target user may interact, and only while unanswered.
  const canAct = !isAnswered && !!currentUserId && currentUserId === meta.target_user;

  // Live selection state (pre-submit). Multi uses a Set; single uses one index.
  const [selectedSet, setSelectedSet] = useState<Set<number>>(new Set());
  const [singleSel, setSingleSel] = useState<number | null>(null);
  const [customChecked, setCustomChecked] = useState(false);
  const [customText, setCustomText] = useState("");

  const targetName = getActorName("member", meta.target_user);

  // --- Answered (terminal) selection, for greying + tick rendering ---
  const answeredIdxSet = new Set<number>([
    ...(existingAnswer?.selected_index != null ? [existingAnswer.selected_index] : []),
    ...(existingAnswer?.selected_indices ?? []),
  ]);
  const answeredCustom = existingAnswer?.custom_text ?? "";

  const isOptionChecked = (i: number): boolean => {
    if (isSubmitted) return answeredIdxSet.has(i);
    return multi ? selectedSet.has(i) : singleSel === i;
  };

  const toggleOption = (i: number) => {
    if (!canAct) return;
    if (multi) {
      setSelectedSet((prev) => {
        const next = new Set(prev);
        if (next.has(i)) next.delete(i);
        else next.add(i);
        return next;
      });
    } else {
      setSingleSel(i);
      // Single-select: picking a real option clears the custom choice.
      setCustomChecked(false);
    }
  };

  const toggleCustom = () => {
    if (!canAct) return;
    if (multi) {
      setCustomChecked((v) => !v);
    } else {
      // Single-select "Other": mutually exclusive with option choice.
      setCustomChecked(true);
      setSingleSel(null);
    }
  };

  // Whether the current live selection is submittable.
  const hasSelection =
    (multi ? selectedSet.size > 0 : singleSel != null) ||
    (allowCustom && customChecked && customText.trim() !== "");

  const submit = () => {
    if (!hasSelection) return;
    const vars: {
      commentId: string;
      state: "submitted";
      selectedIndex?: number;
      selectedIndices?: number[];
      customText?: string;
    } = { commentId, state: "submitted" };

    if (multi) {
      vars.selectedIndices = [...selectedSet].sort((a, b) => a - b);
    } else if (singleSel != null) {
      vars.selectedIndex = singleSel;
    }
    if (allowCustom && customChecked && customText.trim() !== "") {
      vars.customText = customText.trim();
    }

    answer.mutate(vars, {
      onError: (err) =>
        toast.error(
          err instanceof Error && err.message
            ? err.message
            : t(($) => $.ask_user_question.submit_failed),
        ),
    });
  };

  const ignore = () => {
    answer.mutate(
      { commentId, state: "ignored" },
      {
        onError: (err) =>
          toast.error(
            err instanceof Error && err.message
              ? err.message
              : t(($) => $.ask_user_question.ignore_failed),
          ),
      },
    );
  };

  // Shared row renderer for a preset option (radio or checkbox control).
  const renderOptionRow = (i: number) => {
    const opt = options[i]!;
    const checked = isOptionChecked(i);
    const dimmed = isSubmitted && !checked;
    const id = `${commentId}-opt-${i}`;
    return (
      <div
        key={id}
        role="button"
        tabIndex={canAct ? 0 : -1}
        onClick={() => toggleOption(i)}
        onKeyDown={(e) => {
          if (canAct && (e.key === "Enter" || e.key === " ")) {
            e.preventDefault();
            toggleOption(i);
          }
        }}
        className={cn(
          "flex items-start gap-2 rounded-md border border-border/60 bg-background px-2.5 py-2 outline-none",
          canAct && "cursor-pointer hover:bg-muted/50 focus-visible:ring-2 focus-visible:ring-ring",
          dimmed && "opacity-50",
        )}
      >
        {multi ? (
          <Checkbox checked={checked} disabled={!canAct} className="mt-0.5 pointer-events-none" />
        ) : (
          <RadioGroupItem id={id} value={String(i)} className="mt-0.5 pointer-events-none" />
        )}
        <div className="min-w-0 flex-1">
          <div className="text-body font-medium leading-snug">{opt.label}</div>
          <div className="text-caption text-muted-foreground leading-snug">{opt.description}</div>
        </div>
      </div>
    );
  };

  // The "Other" custom row (only when allow_custom).
  const customIsChecked = isSubmitted ? answeredCustom !== "" : customChecked;
  const renderCustomRow = () => {
    const dimmed = isSubmitted && !customIsChecked;
    return (
      <div
        className={cn(
          "rounded-md border border-border/60 bg-background px-2.5 py-2",
          dimmed && "opacity-50",
        )}
      >
        <div
          role="button"
          tabIndex={canAct ? 0 : -1}
          onClick={toggleCustom}
          onKeyDown={(e) => {
            if (canAct && (e.key === "Enter" || e.key === " ")) {
              e.preventDefault();
              toggleCustom();
            }
          }}
          className={cn(
            "flex items-start gap-2 outline-none",
            canAct && "cursor-pointer focus-visible:ring-2 focus-visible:ring-ring",
          )}
        >
          {multi ? (
            <Checkbox checked={customIsChecked} disabled={!canAct} className="mt-0.5 pointer-events-none" />
          ) : (
            <RadioGroupItem
              id={`${commentId}-opt-custom`}
              value="__custom__"
              className="mt-0.5 pointer-events-none"
            />
          )}
          <div className="min-w-0 flex-1 text-body font-medium leading-snug">
            {t(($) => $.ask_user_question.other)}
          </div>
        </div>
        {/* Submitted terminal: show the stored text. Editing: show the input. */}
        {isSubmitted && answeredCustom !== "" && (
          <div className="mt-1.5 pl-6 text-body text-muted-foreground">{answeredCustom}</div>
        )}
        {canAct && customChecked && (
          <div className="mt-1.5 pl-6">
            {/* Plain <input> (not the Base UI <Input>): this is a simple free-text
                field with no Field integration, and Base UI's input event wrapping
                swallows synthetic change events (breaks tests + programmatic fills). */}
            <input
              type="text"
              value={customText}
              onChange={(e) => setCustomText(e.target.value)}
              placeholder={t(($) => $.ask_user_question.custom_placeholder)}
              onClick={(e) => e.stopPropagation()}
              className="h-8 w-full rounded-md border border-input bg-background px-2.5 text-body outline-none focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/50"
            />
          </div>
        )}
      </div>
    );
  };

  const optionList = (
    <div className="mt-3 space-y-2">
      {options.map((_, i) => renderOptionRow(i))}
      {allowCustom && renderCustomRow()}
    </div>
  );

  return (
    <div className={cn("rounded-md border border-border bg-muted/30 p-3", className)}>
      {/* Layer 1 — @target prompt */}
      <p className="text-body">
        <span className="font-semibold text-primary">@{targetName}</span>
        {t(($) => $.ask_user_question.prompt)}
        {multi && (
          <span className="ml-1 text-caption text-muted-foreground">
            {t(($) => $.ask_user_question.multi_hint)}
          </span>
        )}
      </p>

      {/* Layer 2 — question */}
      <p className="mt-2 text-body font-medium text-foreground">{meta.question}</p>

      {/* Layer 3 — options. Radio needs the RadioGroup wrapper; checkbox does not.
          The RadioGroup is kept always-controlled with a stable string value
          ("" when nothing is picked) to avoid Base UI's uncontrolled→controlled
          warning. Actual tick state is driven by isOptionChecked per row, so the
          value here only needs to be a consistent non-undefined string. */}
      {multi ? (
        optionList
      ) : (
        <RadioGroup
          value={
            (isSubmitted
              ? existingAnswer?.selected_index != null
                ? String(existingAnswer.selected_index)
                : answeredCustom !== ""
                  ? "__custom__"
                  : ""
              : customChecked
                ? "__custom__"
                : singleSel != null
                  ? String(singleSel)
                  : "")
          }
          disabled={!canAct}
        >
          {optionList}
        </RadioGroup>
      )}

      {/* Layer 4 — actions / terminal state */}
      <div className="mt-3 flex items-center gap-2">
        {isSubmitted && (
          <span className="inline-flex items-center gap-1 rounded-md bg-muted px-2.5 py-1 text-caption font-medium text-muted-foreground">
            <CheckCircle2 className="size-3.5" />
            {t(($) => $.ask_user_question.submitted)}
          </span>
        )}
        {isIgnored && (
          <span className="inline-flex items-center gap-1 rounded-md bg-muted px-2.5 py-1 text-caption font-medium text-muted-foreground">
            <XCircle className="size-3.5" />
            {t(($) => $.ask_user_question.ignored)}
          </span>
        )}
        {canAct && (
          <>
            <Button
              size="sm"
              variant="outline"
              disabled={!hasSelection || answer.isPending}
              onClick={submit}
            >
              {t(($) => $.ask_user_question.submit)}
            </Button>
            <Button
              size="sm"
              variant="ghost"
              disabled={answer.isPending}
              onClick={ignore}
            >
              {t(($) => $.ask_user_question.ignore)}
            </Button>
          </>
        )}
      </div>
    </div>
  );
}

export const AskUserQuestionCard = memo(AskUserQuestionCardImpl);
