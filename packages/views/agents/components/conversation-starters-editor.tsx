"use client";

import { Plus, Trash2 } from "lucide-react";
import {
  AGENT_CONVERSATION_STARTER_LABEL_MAX_LENGTH,
  AGENT_CONVERSATION_STARTER_MAX_LENGTH,
  AGENT_CONVERSATION_STARTERS_MAX,
  selectConversationStarters,
} from "@multica/core/agents";
import type { AgentConversationStarter } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  ConversationStarterList,
  useFallbackConversationStarters,
} from "../../chat/components/conversation-starter-list";
import { useT } from "../../i18n";

export function ConversationStartersEditor({
  value,
  onChange,
  disabled = false,
}: {
  value: AgentConversationStarter[];
  onChange: (value: AgentConversationStarter[]) => void;
  disabled?: boolean;
}) {
  const { t } = useT("agents");
  const hasIncompletePrompt = value.some(
    (item) => !item.label.trim() || !item.prompt.trim(),
  );
  // Rendered through the same component and the same resolver the chat empty
  // state uses, so the preview cannot drift from what it previews — including
  // the fallback rule, which is how an author learns that the three
  // suggestions they never configured are defaults they can replace.
  const fallbackStarters = useFallbackConversationStarters();
  const preview = selectConversationStarters(value, fallbackStarters);

  const update = (
    index: number,
    field: keyof AgentConversationStarter,
    nextValue: string,
  ) => {
    onChange(
      value.map((item, itemIndex) =>
        itemIndex === index ? { ...item, [field]: nextValue } : item,
      ),
    );
  };

  return (
    <div className="space-y-3">
      <div>
        <p className="text-body font-medium">
          {t(($) => $.conversation_starters.label)}
        </p>
        <p className="mt-1 text-caption leading-5 text-muted-foreground">
          {t(($) => $.conversation_starters.hint)}
        </p>
      </div>

      {value.map((item, index) => (
        <div key={index} className="rounded-lg border bg-muted/20 p-3">
          <div className="flex items-center gap-2">
            <Input
              value={item.label}
              maxLength={AGENT_CONVERSATION_STARTER_LABEL_MAX_LENGTH}
              disabled={disabled}
              aria-label={t(($) => $.conversation_starters.item_label, {
                number: index + 1,
              })}
              placeholder={t(($) => $.conversation_starters.label_placeholder)}
              onChange={(event) => update(index, "label", event.target.value)}
            />
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              disabled={disabled}
              aria-label={t(($) => $.conversation_starters.remove, {
                number: index + 1,
              })}
              onClick={() =>
                onChange(value.filter((_, itemIndex) => itemIndex !== index))
              }
            >
              <Trash2 className="size-4" aria-hidden="true" />
            </Button>
          </div>
          <Textarea
            value={item.prompt}
            maxLength={AGENT_CONVERSATION_STARTER_MAX_LENGTH}
            disabled={disabled}
            rows={3}
            className="mt-2 resize-y"
            aria-label={t(($) => $.conversation_starters.prompt_label, {
              number: index + 1,
            })}
            placeholder={t(($) => $.conversation_starters.prompt_placeholder)}
            onChange={(event) => update(index, "prompt", event.target.value)}
          />
        </div>
      ))}

      {value.length < AGENT_CONVERSATION_STARTERS_MAX ? (
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={disabled}
          onClick={() => onChange([...value, { label: "", prompt: "" }])}
        >
          <Plus className="size-4" aria-hidden="true" />
          {t(($) => $.conversation_starters.add)}
        </Button>
      ) : null}

      {hasIncompletePrompt ? (
        <p className="text-caption text-destructive" role="alert">
          {t(($) => $.conversation_starters.incomplete)}
        </p>
      ) : null}

      <div className="rounded-lg border border-dashed bg-muted/20 px-3 py-3">
        <p className="text-caption font-medium text-muted-foreground">
          {t(($) => $.conversation_starters.preview_label)}
        </p>
        <ConversationStarterList
          className="mt-2 max-w-sm"
          starters={preview.starters}
        />
        {preview.isFallback ? (
          <p className="mt-2 text-caption leading-5 text-muted-foreground">
            {t(($) => $.conversation_starters.preview_defaults)}
          </p>
        ) : null}
      </div>
    </div>
  );
}
