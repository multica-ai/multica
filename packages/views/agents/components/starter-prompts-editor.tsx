"use client";

import { Plus, Trash2 } from "lucide-react";
import {
  AGENT_STARTER_PROMPT_LABEL_MAX_LENGTH,
  AGENT_STARTER_PROMPT_MAX_LENGTH,
  AGENT_STARTER_PROMPTS_MAX,
  selectStarterPrompts,
} from "@multica/core/agents";
import type { AgentStarterPrompt } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  StarterPromptList,
  useFallbackStarterPrompts,
} from "../../chat/components/starter-prompt-list";
import { useT } from "../../i18n";

export function StarterPromptsEditor({
  value,
  onChange,
  disabled = false,
}: {
  value: AgentStarterPrompt[];
  onChange: (value: AgentStarterPrompt[]) => void;
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
  const fallbackPrompts = useFallbackStarterPrompts();
  const preview = selectStarterPrompts(value, fallbackPrompts);

  const update = (
    index: number,
    field: keyof AgentStarterPrompt,
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
          {t(($) => $.starter_prompts.label)}
        </p>
        <p className="mt-1 text-caption leading-5 text-muted-foreground">
          {t(($) => $.starter_prompts.hint)}
        </p>
      </div>

      {value.map((item, index) => (
        <div key={index} className="rounded-lg border bg-muted/20 p-3">
          <div className="flex items-center gap-2">
            <Input
              value={item.label}
              maxLength={AGENT_STARTER_PROMPT_LABEL_MAX_LENGTH}
              disabled={disabled}
              aria-label={t(($) => $.starter_prompts.item_label, {
                number: index + 1,
              })}
              placeholder={t(($) => $.starter_prompts.label_placeholder)}
              onChange={(event) => update(index, "label", event.target.value)}
            />
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              disabled={disabled}
              aria-label={t(($) => $.starter_prompts.remove, {
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
            maxLength={AGENT_STARTER_PROMPT_MAX_LENGTH}
            disabled={disabled}
            rows={3}
            className="mt-2 resize-y"
            aria-label={t(($) => $.starter_prompts.prompt_label, {
              number: index + 1,
            })}
            placeholder={t(($) => $.starter_prompts.prompt_placeholder)}
            onChange={(event) => update(index, "prompt", event.target.value)}
          />
        </div>
      ))}

      {value.length < AGENT_STARTER_PROMPTS_MAX ? (
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={disabled}
          onClick={() => onChange([...value, { label: "", prompt: "" }])}
        >
          <Plus className="size-4" aria-hidden="true" />
          {t(($) => $.starter_prompts.add)}
        </Button>
      ) : null}

      {hasIncompletePrompt ? (
        <p className="text-caption text-destructive" role="alert">
          {t(($) => $.starter_prompts.incomplete)}
        </p>
      ) : null}

      <div className="rounded-lg border border-dashed bg-muted/20 px-3 py-3">
        <p className="text-caption font-medium text-muted-foreground">
          {t(($) => $.starter_prompts.preview_label)}
        </p>
        <StarterPromptList
          className="mt-2 max-w-sm"
          prompts={preview.prompts}
        />
        {preview.isFallback ? (
          <p className="mt-2 text-caption leading-5 text-muted-foreground">
            {t(($) => $.starter_prompts.preview_defaults)}
          </p>
        ) : null}
      </div>
    </div>
  );
}
