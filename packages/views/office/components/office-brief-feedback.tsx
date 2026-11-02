"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  BRIEF_FEEDBACK_TAGS,
  briefFeedbackSummaryOptions,
  myBriefFeedbackOptions,
  useSubmitBriefFeedback,
  type BriefFeedback,
  type BriefFeedbackRating,
  type BriefFeedbackTag,
} from "@multica/core/office";
import { useWorkspaceId } from "@multica/core/hooks";
import { Button } from "@multica/ui/components/ui/button";
import { Loader2 } from "@multica/ui/icons";
import { useT } from "../../i18n";

const NOTE_MAX_LENGTH = 500;

const TAG_KEYS = {
  too_long: "feedback_tag_too_long",
  missing_something: "feedback_tag_missing_something",
  wrong: "feedback_tag_wrong",
  too_vague: "feedback_tag_too_vague",
  not_for_me: "feedback_tag_not_for_me",
} as const satisfies Record<BriefFeedbackTag, string>;

/**
 * The member's rating of a brief (spec 2.8). The rating is required; tags and a
 * note are optional, and tags are offered only for "Not useful". Owners and admins
 * also see the workspace's rolling usefulness; the figure is hidden for anyone the
 * server refuses it to, and while it has no ratings.
 */
export function OfficeBriefFeedback({ briefId, canManage }: { briefId: string; canManage: boolean }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const mine = useQuery(myBriefFeedbackOptions(wsId, briefId));
  const summary = useQuery({
    ...briefFeedbackSummaryOptions(wsId),
    enabled: canManage && !!wsId,
  });
  const usefulness = summary.data?.usefulness ?? null;

  if (mine.isPending) return null;
  return (
    <section aria-label={t(($) => $.office.feedback_question)} className="flex flex-col gap-3 rounded-lg border px-4 py-3">
      {/* Keyed on the stored rating, so the form restarts from what the server saved. */}
      <FeedbackForm key={mine.data?.updated_at ?? "none"} briefId={briefId} saved={mine.data ?? null} />
      {canManage && usefulness !== null ? (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.office.feedback_usefulness, {
            percent: Math.round(usefulness * 100),
            days: summary.data?.days ?? 14,
          })}
        </p>
      ) : null}
    </section>
  );
}

function FeedbackForm({ briefId, saved }: { briefId: string; saved: BriefFeedback | null }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const submit = useSubmitBriefFeedback(wsId, briefId);
  const [rating, setRating] = useState<BriefFeedbackRating | null>(saved?.rating ?? null);
  const [tags, setTags] = useState<BriefFeedbackTag[]>(saved?.tags ?? []);
  const [note, setNote] = useState(saved?.note ?? "");

  const toggleTag = (tag: BriefFeedbackTag) =>
    setTags((current) => (current.includes(tag) ? current.filter((item) => item !== tag) : [...current, tag]));

  const onSave = () => {
    if (!rating) return;
    submit.mutate(
      { rating, tags: rating === "not_useful" ? tags : [], note: note.trim() },
      {
        onSuccess: () => toast.success(t(($) => $.office.feedback_saved)),
        onError: () => toast.error(t(($) => $.office.feedback_save_failed)),
      },
    );
  };

  return (
    <div className="flex flex-col gap-3">
      <div role="group" aria-label={t(($) => $.office.feedback_question)} className="flex flex-wrap gap-2">
        <Button
          type="button"
          size="sm"
          variant={rating === "useful" ? "brand" : "outline"}
          aria-pressed={rating === "useful"}
          onClick={() => setRating("useful")}
        >
          {t(($) => $.office.feedback_useful)}
        </Button>
        <Button
          type="button"
          size="sm"
          variant={rating === "not_useful" ? "brand" : "outline"}
          aria-pressed={rating === "not_useful"}
          onClick={() => setRating("not_useful")}
        >
          {t(($) => $.office.feedback_not_useful)}
        </Button>
      </div>
      {rating === "not_useful" ? (
        <div role="group" aria-label={t(($) => $.office.feedback_tags_label)} className="flex flex-wrap gap-2">
          {BRIEF_FEEDBACK_TAGS.map((tag) => {
            const selected = tags.includes(tag);
            return (
              <Button
                key={tag}
                type="button"
                size="xs"
                variant={selected ? "brand" : "outline"}
                aria-pressed={selected}
                onClick={() => toggleTag(tag)}
              >
                {t(($) => $.office[TAG_KEYS[tag]])}
              </Button>
            );
          })}
        </div>
      ) : null}
      <label className="flex flex-col gap-1">
        <span className="text-caption text-muted-foreground">{t(($) => $.office.feedback_note_label)}</span>
        <textarea
          value={note}
          maxLength={NOTE_MAX_LENGTH}
          rows={2}
          onChange={(event) => setNote(event.target.value)}
          className="resize-y rounded-lg border border-input bg-background px-3 py-2 text-body text-foreground focus-visible:outline-2 focus-visible:outline-ring"
        />
      </label>
      <div className="flex justify-end">
        <Button type="button" size="sm" disabled={!rating || submit.isPending} aria-busy={submit.isPending} onClick={onSave}>
          {submit.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
          {t(($) => $.office.feedback_save)}
        </Button>
      </div>
    </div>
  );
}
