import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { ConfirmationDialog, Input } from "@tutti-os/ui-system";
import { observer } from "mobx-react-lite";
import { memo, useCallback, useEffect, useRef, useState } from "react";
import { Button } from "../../../app/renderer/components/ui/button";
import type { AgentGUIRailViewModel } from "../model/agentGuiNodeTypes";
import type { AgentGUIViewLabels } from "./AgentGUINodeView.types";

interface AgentGUIRenameConversationDialogProps {
  conversation: AgentGUIRailViewModel["conversations"][number] | null;
  open: boolean;
  labels: AgentGUIViewLabels;
  onOpenChange: (open: boolean) => void;
  onRename: (agentSessionId: string, title: string) => Promise<void>;
}

export const AgentGUIRenameConversationDialog = observer(
  function AgentGUIRenameConversationDialog({
    conversation,
    open,
    labels,
    onOpenChange,
    onRename
  }: AgentGUIRenameConversationDialogProps): React.JSX.Element {
    "use memo";
    const [title, setTitle] = useState("");
    const [isSaving, setIsSaving] = useState(false);
    const isSavingRef = useRef(false);
    const armedPointerActionRef = useRef<"cancel" | "confirm" | null>(null);
    const inputRef = useRef<HTMLInputElement | null>(null);
    const trimmedTitle = title.trim();
    useEffect(() => {
      if (!open || !conversation) {
        isSavingRef.current = false;
        armedPointerActionRef.current = null;
        setIsSaving(false);
        return;
      }
      setTitle(conversation.title);
    }, [conversation, open]);
    useEffect(() => {
      if (!open) {
        return;
      }
      // timing: defer focus until after the dialog's open animation mounts the input
      const timer = window.setTimeout(() => {
        inputRef.current?.focus();
        inputRef.current?.select();
      }, 0);
      return () => window.clearTimeout(timer);
    }, [open, conversation?.id]);
    const closeRenameDialog = useCallback(() => {
      if (!isSavingRef.current) {
        onOpenChange(false);
      }
    }, [onOpenChange]);
    const closeRenameDialogCallback = usePersistCallback(closeRenameDialog);
    const confirmRename = useCallback(() => {
      if (!conversation || isSavingRef.current || !trimmedTitle) {
        return;
      }
      isSavingRef.current = true;
      setIsSaving(true);
      void onRename(conversation.id, trimmedTitle)
        .then(() => {
          onOpenChange(false);
        })
        .catch(() => {
          inputRef.current?.focus();
        })
        .finally(() => {
          isSavingRef.current = false;
          setIsSaving(false);
        });
    }, [conversation, onOpenChange, onRename, trimmedTitle]);
    const confirmRenameCallback = usePersistCallback(confirmRename);
    const onLostPointerCaptureProp1 = usePersistCallback<
      NonNullable<React.ComponentProps<typeof Button>["onLostPointerCapture"]>
    >(() => {
      if (armedPointerActionRef.current === "cancel") {
        armedPointerActionRef.current = null;
      }
    });
    const onPointerCancelProp2 = usePersistCallback<
      NonNullable<React.ComponentProps<typeof Button>["onPointerCancel"]>
    >(() => {
      if (armedPointerActionRef.current === "cancel") {
        armedPointerActionRef.current = null;
      }
    });
    const onPointerDownProp3 = usePersistCallback<
      NonNullable<React.ComponentProps<typeof Button>["onPointerDown"]>
    >((event) => {
      if (event.button === 0) {
        armedPointerActionRef.current = "cancel";
      }
    });
    const onPointerLeaveProp4 = usePersistCallback<
      NonNullable<React.ComponentProps<typeof Button>["onPointerLeave"]>
    >(() => {
      if (armedPointerActionRef.current === "cancel") {
        armedPointerActionRef.current = null;
      }
    });
    const onPointerUpProp5 = usePersistCallback<
      NonNullable<React.ComponentProps<typeof Button>["onPointerUp"]>
    >((event) => {
      const isArmed = armedPointerActionRef.current === "cancel";
      armedPointerActionRef.current = null;
      if (event.button === 0 && isArmed) {
        closeRenameDialog();
      }
    });
    const onClickProp6 = usePersistCallback<
      NonNullable<React.ComponentProps<typeof Button>["onClick"]>
    >((event) => {
      if (event.detail !== 0) {
        return;
      }
      confirmRename();
    });
    const onKeyDownProp7 = usePersistCallback<
      NonNullable<React.ComponentProps<typeof Button>["onKeyDown"]>
    >((event) => {
      if ((event.key === "Enter" || event.key === " ") && !event.repeat) {
        event.preventDefault();
        confirmRename();
      }
    });
    const onLostPointerCaptureProp8 = usePersistCallback<
      NonNullable<React.ComponentProps<typeof Button>["onLostPointerCapture"]>
    >(() => {
      if (armedPointerActionRef.current === "confirm") {
        armedPointerActionRef.current = null;
      }
    });
    const onPointerCancelProp9 = usePersistCallback<
      NonNullable<React.ComponentProps<typeof Button>["onPointerCancel"]>
    >(() => {
      if (armedPointerActionRef.current === "confirm") {
        armedPointerActionRef.current = null;
      }
    });
    const onPointerDownProp10 = usePersistCallback<
      NonNullable<React.ComponentProps<typeof Button>["onPointerDown"]>
    >((event) => {
      if (event.button === 0) {
        armedPointerActionRef.current = "confirm";
      }
    });
    const onPointerLeaveProp11 = usePersistCallback<
      NonNullable<React.ComponentProps<typeof Button>["onPointerLeave"]>
    >(() => {
      if (armedPointerActionRef.current === "confirm") {
        armedPointerActionRef.current = null;
      }
    });
    const onPointerUpProp12 = usePersistCallback<
      NonNullable<React.ComponentProps<typeof Button>["onPointerUp"]>
    >((event) => {
      const isArmed = armedPointerActionRef.current === "confirm";
      armedPointerActionRef.current = null;
      if (event.button === 0 && isArmed) {
        confirmRename();
      }
    });
    const onChangeProp13 = usePersistCallback<
      NonNullable<React.ComponentProps<typeof Input>["onChange"]>
    >((event) => setTitle(event.currentTarget.value));
    const onKeyDownProp14 = usePersistCallback<
      NonNullable<React.ComponentProps<typeof Input>["onKeyDown"]>
    >((event) => {
      if (event.key === "Enter") {
        event.preventDefault();
        confirmRename();
      }
    });
    return (
      <ConfirmationDialog
        cancelLabel={labels.cancel}
        className="sm:max-w-[480px]"
        confirmBusy={isSaving}
        confirmDisabled={!trimmedTitle}
        confirmLabel={labels.renameSessionSave}
        description={labels.renameSessionDescription}
        footer={
          <div className="flex justify-end gap-2">
            <Button
              disabled={isSaving}
              size="dialog"
              type="button"
              variant="ghost"
              onClick={closeRenameDialogCallback}
              onLostPointerCapture={onLostPointerCaptureProp1}
              onPointerCancel={onPointerCancelProp2}
              onPointerDown={onPointerDownProp3}
              onPointerLeave={onPointerLeaveProp4}
              onPointerUp={onPointerUpProp5}
            >
              {labels.cancel}
            </Button>
            <Button
              className="shadow-none"
              disabled={isSaving || !trimmedTitle}
              size="dialog"
              type="button"
              variant="default"
              onClick={onClickProp6}
              onKeyDown={onKeyDownProp7}
              onLostPointerCapture={onLostPointerCaptureProp8}
              onPointerCancel={onPointerCancelProp9}
              onPointerDown={onPointerDownProp10}
              onPointerLeave={onPointerLeaveProp11}
              onPointerUp={onPointerUpProp12}
            >
              {labels.renameSessionSave}
            </Button>
          </div>
        }
        open={open}
        title={labels.renameSessionTitle}
        onConfirm={confirmRenameCallback}
        onOpenChange={onOpenChange}
      >
        <Input
          ref={inputRef}
          aria-label={labels.renameSessionTitle}
          className="h-9"
          variant="md"
          placeholder={labels.renameSessionPlaceholder}
          value={title}
          onChange={onChangeProp13}
          onKeyDown={onKeyDownProp14}
        />
      </ConfirmationDialog>
    );
  }
);
