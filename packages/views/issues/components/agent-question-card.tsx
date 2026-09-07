"use client";

import { useCallback, useMemo, useState } from "react";
import { CheckCircle2, Loader2, MessageCircleQuestion } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Label } from "@multica/ui/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@multica/ui/components/ui/radio-group";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { cn } from "@multica/ui/lib/utils";
import type { AgentQuestion, AgentQuestionPayload, TimelineEntry } from "@multica/core/types";
import { useT } from "../../i18n";

// The sentinel value the "Other" choice uses inside a question's selection.
// Never rendered; the free-text answer replaces it in the posted reply.
const OTHER = "__other__";

interface QuestionAnswer {
  selected: string[];
  other: string;
}

/**
 * Returns the structured question carried by a timeline entry, or null when
 * the entry is not an agent question (or the payload is unreadable). The
 * markdown body remains the fallback rendering in that case.
 */
export function agentQuestionOf(entry: Pick<TimelineEntry, "question_payload">): AgentQuestionPayload | null {
  const payload = entry.question_payload;
  if (!payload || !Array.isArray(payload.questions) || payload.questions.length === 0) return null;
  return payload;
}

/**
 * True when a workspace member has replied anywhere in the question's thread.
 * That reply is the answer: the agent's next run reads it, so the card
 * becomes read-only and the inbox item is retired server-side.
 */
export function agentQuestionAnswered(replies: readonly Pick<TimelineEntry, "actor_type">[]): boolean {
  return replies.some((reply) => reply.actor_type === "member");
}

function questionKey(question: AgentQuestion, index: number): string {
  return `${index}:${question.question}`;
}

// Option labels are free text (`--dry-run`, `Health checks`); an id with
// spaces breaks the label/control association, so the DOM id is derived from
// the option's position, never its text.
function optionId(entryId: string, questionIndex: number, optionIndex: number | "other"): string {
  return `agent-question-${entryId}-${questionIndex}-${optionIndex}`;
}

function answerComplete(answer: QuestionAnswer | undefined): boolean {
  if (!answer) return false;
  const picked = answer.selected.filter((value) => value !== OTHER);
  const usesOther = answer.selected.includes(OTHER);
  if (usesOther && answer.other.trim() === "") return false;
  return picked.length > 0 || usesOther;
}

/**
 * Renders the posted answer. One line per question, `**Header:** answer`,
 * so the agent — which reads the reply as plain comment text on its next
 * run — sees every question paired with its choice, and so do humans in
 * every surface without the card (mobile, inbox, chat integrations).
 */
export function formatAgentQuestionAnswer(payload: AgentQuestionPayload, answers: Record<string, QuestionAnswer>): string {
  return payload.questions
    .map((question, index) => {
      const answer = answers[questionKey(question, index)];
      const values: string[] = [];
      for (const value of answer?.selected ?? []) {
        if (value === OTHER) {
          const other = answer?.other.trim() ?? "";
          if (other) values.push(other);
        } else {
          values.push(value);
        }
      }
      const label = question.header?.trim() || question.question;
      return `**${label}:** ${values.join(", ")}`;
    })
    .join("\n");
}

interface AgentQuestionCardProps {
  entry: TimelineEntry;
  /** Flat list of every reply under this thread root (see CommentCard). */
  replies: TimelineEntry[];
  agentName: string;
  /** Posts the answer as a reply in this thread; resolves to the new
   *  comment id on success, false on failure (same contract as ReplyInput). */
  onSubmit: (content: string) => Promise<string | boolean>;
  onAccepted?: (commentId: string) => void;
  className?: string;
}

/**
 * Interactive card for an agent question comment (GitHub #8048). Mirrors
 * Claude Code's AskUserQuestion prompt: one block per question with radio
 * or checkbox options by `multi_select`, an "Other" free-text choice, and a
 * single Submit that posts the answers as a thread reply.
 */
export function AgentQuestionCard({ entry, replies, agentName, onSubmit, onAccepted, className }: AgentQuestionCardProps) {
  const { t } = useT("issues");
  const payload = agentQuestionOf(entry);
  const answered = useMemo(() => agentQuestionAnswered(replies), [replies]);
  const [answers, setAnswers] = useState<Record<string, QuestionAnswer>>({});
  const [sending, setSending] = useState(false);

  const setAnswer = useCallback((key: string, update: (prev: QuestionAnswer) => QuestionAnswer) => {
    setAnswers((prev) => ({ ...prev, [key]: update(prev[key] ?? { selected: [], other: "" }) }));
  }, []);

  const complete = useMemo(
    () => payload !== null && payload.questions.every((question, index) => answerComplete(answers[questionKey(question, index)])),
    [payload, answers],
  );

  const handleSubmit = useCallback(async () => {
    if (!payload || !complete || sending) return;
    setSending(true);
    try {
      const result = await onSubmit(formatAgentQuestionAnswer(payload, answers));
      if (result === false) {
        toast.error(t(($) => $.agent_question.submit_failed));
        return;
      }
      if (typeof result === "string") onAccepted?.(result);
    } finally {
      setSending(false);
    }
  }, [payload, complete, sending, onSubmit, answers, onAccepted, t]);

  if (!payload) return null;
  const disabled = answered || sending;

  return (
    <div className={cn("flex flex-col gap-3", className)} data-testid="agent-question-card">
      <div className="flex items-center gap-2 text-caption text-muted-foreground">
        <MessageCircleQuestion className="size-3.5" aria-hidden />
        <span>{agentName ? t(($) => $.agent_question.title, { name: agentName }) : t(($) => $.agent_question.title_fallback)}</span>
        {answered && (
          <Badge variant="secondary" className="gap-1">
            <CheckCircle2 className="size-3" aria-hidden />
            {t(($) => $.agent_question.answered)}
          </Badge>
        )}
      </div>

      {payload.questions.map((question, index) => {
        const key = questionKey(question, index);
        const answer = answers[key] ?? { selected: [], other: "" };
        const usesOther = answer.selected.includes(OTHER);
        const groupLabel = t(($) => $.agent_question.option_group_label, { question: question.question });
        return (
          <fieldset key={key} className="flex flex-col gap-2 rounded-md border border-border p-3" disabled={disabled}>
            <legend className="sr-only">{groupLabel}</legend>
            <div className="flex flex-col gap-0.5">
              {question.header && <span className="text-caption font-medium uppercase tracking-wide text-muted-foreground">{question.header}</span>}
              <span className="text-body font-medium text-foreground">{question.question}</span>
              <span className="text-caption text-muted-foreground">
                {question.multi_select === true ? t(($) => $.agent_question.pick_multiple) : t(($) => $.agent_question.pick_single)}
              </span>
            </div>

            {question.multi_select === true ? (
              <div className="grid gap-2" role="group" aria-label={groupLabel}>
                {question.options.map((option, optionIndex) => {
                  const id = optionId(entry.id, index, optionIndex);
                  const checked = answer.selected.includes(option.label);
                  return (
                    <div key={option.label} className="flex items-start gap-2">
                      <Checkbox
                        id={id}
                        checked={checked}
                        disabled={disabled}
                        onCheckedChange={(next) =>
                          setAnswer(key, (prev) => ({
                            ...prev,
                            selected: next ? [...prev.selected, option.label] : prev.selected.filter((value) => value !== option.label),
                          }))
                        }
                      />
                      <OptionLabel htmlFor={id} label={option.label} description={option.description} />
                    </div>
                  );
                })}
                <div className="flex items-start gap-2">
                  <Checkbox
                    id={optionId(entry.id, index, "other")}
                    checked={usesOther}
                    disabled={disabled}
                    onCheckedChange={(next) =>
                      setAnswer(key, (prev) => ({
                        ...prev,
                        selected: next ? [...prev.selected, OTHER] : prev.selected.filter((value) => value !== OTHER),
                      }))
                    }
                  />
                  <OptionLabel htmlFor={optionId(entry.id, index, "other")} label={t(($) => $.agent_question.other_option)} />
                </div>
              </div>
            ) : (
              <RadioGroup
                aria-label={groupLabel}
                value={answer.selected[0] ?? null}
                disabled={disabled}
                onValueChange={(value) => setAnswer(key, (prev) => ({ ...prev, selected: typeof value === "string" ? [value] : [] }))}
              >
                {question.options.map((option, optionIndex) => {
                  const id = optionId(entry.id, index, optionIndex);
                  return (
                    <div key={option.label} className="flex items-start gap-2">
                      <RadioGroupItem id={id} value={option.label} disabled={disabled} />
                      <OptionLabel htmlFor={id} label={option.label} description={option.description} />
                    </div>
                  );
                })}
                <div className="flex items-start gap-2">
                  <RadioGroupItem id={optionId(entry.id, index, "other")} value={OTHER} disabled={disabled} />
                  <OptionLabel htmlFor={optionId(entry.id, index, "other")} label={t(($) => $.agent_question.other_option)} />
                </div>
              </RadioGroup>
            )}

            {usesOther && !answered && (
              <Textarea
                value={answer.other}
                disabled={disabled}
                placeholder={t(($) => $.agent_question.other_placeholder)}
                aria-label={t(($) => $.agent_question.other_option)}
                rows={2}
                onChange={(event) => setAnswer(key, (prev) => ({ ...prev, other: event.target.value }))}
              />
            )}
          </fieldset>
        );
      })}

      {!answered && (
        <div className="flex items-center gap-3">
          <Button size="sm" onClick={handleSubmit} disabled={!complete || sending} aria-busy={sending || undefined}>
            {sending && <Loader2 className="size-3.5 animate-spin" aria-hidden />}
            {sending ? t(($) => $.agent_question.sending) : t(($) => $.agent_question.submit)}
          </Button>
          <span className="text-caption text-muted-foreground">{t(($) => $.agent_question.resume_hint)}</span>
        </div>
      )}
    </div>
  );
}

function OptionLabel({ htmlFor, label, description }: { htmlFor: string; label: string; description?: string }) {
  return (
    <Label htmlFor={htmlFor} className="flex cursor-pointer flex-col items-start gap-0.5 font-normal leading-snug">
      <span className="text-body text-foreground">{label}</span>
      {description && <span className="text-caption text-muted-foreground">{description}</span>}
    </Label>
  );
}
