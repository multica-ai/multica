import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import type { JSX, KeyboardEvent } from "react";
import { Button } from "@tutti-os/ui-system";
import styles from "../../../agent-gui/agentGuiNode/AgentGUIConversation.styles";
import { observer } from "mobx-react-lite";
export const AgentUserMessageEditor = observer(AgentUserMessageEditorRender);

export interface AgentUserMessageEditRetryControl {
  pending: boolean;
  labels: {
    edit: string;
    cancel: string;
    submit: string;
  };
  onSubmit: (input: {
    editedText: string;
    turnId: string;
  }) => boolean | Promise<boolean>;
}

function AgentUserMessageEditorRender({
  value,
  pending,
  labels,
  onChange,
  onCancel,
  onSubmit,
  onKeyDown
}: AgentUserMessageEditorProps): JSX.Element {
  "use memo";
  const onChangeProp1 = usePersistCallback<
    NonNullable<React.ComponentProps<"textarea">["onChange"]>
  >((event) => onChange(event.currentTarget.value));
  const onClickProp2 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => void onSubmit());
  return (
    <div
      className={`workspace-agents-status-panel__detail-user-message ${styles.userMessageBubble} ${styles.userMessageEditor}`}
      aria-busy={pending ? "true" : undefined}
    >
      <textarea
        autoFocus
        className={styles.userMessageEditorTextarea}
        value={value}
        disabled={pending}
        aria-label={labels.edit}
        onChange={onChangeProp1}
        onKeyDown={onKeyDown}
      />
      <div className={styles.userMessageEditorActions}>
        <Button
          className="rounded-[var(--radius-xl)] border-[var(--line-2)] dark:border-[var(--line-2)]"
          disabled={pending}
          size="sm"
          type="button"
          variant="outline"
          onClick={onCancel}
        >
          {labels.cancel}
        </Button>
        <Button
          className="rounded-[var(--radius-xl)]"
          disabled={pending || value.trim() === ""}
          size="sm"
          type="button"
          variant="default"
          onClick={onClickProp2}
        >
          {labels.submit}
        </Button>
      </div>
    </div>
  );
}

type AgentUserMessageEditorProps = {
  value: string;
  pending: boolean;
  labels: AgentUserMessageEditRetryControl["labels"];
  onChange: (value: string) => void;
  onCancel: () => void;
  onSubmit: () => Promise<void>;
  onKeyDown: (event: KeyboardEvent<HTMLTextAreaElement>) => void;
};
