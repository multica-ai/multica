"use client";

import { useEffect, useState } from "react";
import { toast } from "sonner";
import { Trash2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Label } from "@multica/ui/components/ui/label";
import { useT } from "../../i18n";
import { useWorkspaceId } from "@multica/core/hooks";
import { useUpdateNode, useDeleteNode } from "@multica/core/workflows/queries";
import { AssigneePicker } from "../../issues/components/pickers/assignee-picker";
import type { WorkflowNode, WorkerType, CriticType, UpdateNodeRequest } from "@multica/core/types";
import type { IssueAssigneeType } from "@multica/core/types/issue";

function toAssigneeType(t: string): IssueAssigneeType | null {
  if (t === "human") return "member";
  if (t === "agent" || t === "squad") return t as IssueAssigneeType;
  return null;
}

function fromAssigneeType(t: IssueAssigneeType | null): WorkerType {
  if (t === "member") return "human";
  if (t === "agent") return "agent";
  if (t === "squad") return "squad";
  return "human";
}

function fromAssigneeTypeCritic(t: IssueAssigneeType | null): CriticType {
  if (t === "member") return "human";
  if (t === "agent") return "agent";
  if (t === "squad") return "squad";
  return "human";
}

interface NodeConfigPanelProps {
  node: WorkflowNode;
  workflowId: string;
  onClose: () => void;
}

export function NodeConfigPanel({ node, workflowId, onClose }: NodeConfigPanelProps) {
  const { t } = useT("workflows");
  const wsId = useWorkspaceId();
  const updateMutation = useUpdateNode(wsId, workflowId);
  const deleteMutation = useDeleteNode(wsId, workflowId);

  const [title, setTitle] = useState(node.title);
  const [description, setDescription] = useState(node.description);
  const [formatSchema, setFormatSchema] = useState(
    node.format_schema ? JSON.stringify(node.format_schema, null, 2) : ""
  );
  const [workerType, setWorkerType] = useState(node.worker_type);
  const [workerId, setWorkerId] = useState<string | null>(node.worker_id ?? null);
  const [criticType, setCriticType] = useState(node.critic_type);
  const [criticId, setCriticId] = useState<string | null>(node.critic_id ?? null);
  const [criticApiUrl, setCriticApiUrl] = useState(node.critic_api_url ?? "");

  useEffect(() => {
    setTitle(node.title);
    setDescription(node.description);
    setFormatSchema(node.format_schema ? JSON.stringify(node.format_schema, null, 2) : "");
    setWorkerType(node.worker_type);
    setWorkerId(node.worker_id ?? null);
    setCriticType(node.critic_type);
    setCriticId(node.critic_id ?? null);
    setCriticApiUrl(node.critic_api_url ?? "");
  }, [node]);

  const handleSave = async () => {
    let formatSchemaParsed: unknown = null;
    if (formatSchema.trim()) {
      try {
        formatSchemaParsed = JSON.parse(formatSchema);
      } catch {
        return;
      }
    }

    const req: UpdateNodeRequest = {
      title,
      description,
      format_schema: formatSchemaParsed,
      worker_type: workerType,
      worker_id: workerId,
      critic_type: criticType,
      critic_id: criticId,
      critic_api_url: criticApiUrl || null,
    };

    try {
      await updateMutation.mutateAsync({ nodeId: node.id, ...req });
      toast.success(t(($) => $.node.toast_saved));
    } catch {
      toast.error(t(($) => $.node.toast_save_failed));
    }
  };

  const handleDelete = async () => {
    try {
      await deleteMutation.mutateAsync(node.id);
      toast.success(t(($) => $.node.toast_deleted));
      onClose();
    } catch {
      toast.error(t(($) => $.node.toast_delete_failed));
    }
  };

  return (
    <div className="flex flex-col h-full border-l bg-card">
      <div className="flex items-center justify-between px-4 py-3 border-b shrink-0">
        <h3 className="text-sm font-medium">{t(($) => $.node.title)}</h3>
        <Button variant="ghost" size="icon" className="h-7 w-7" onClick={onClose}>
          <svg width="15" height="15" viewBox="0 0 15 15" fill="none">
            <path d="M11.7816 4.03157C12.0062 3.80702 12.0062 3.44295 11.7816 3.2184C11.5571 2.99385 11.193 2.99385 10.9685 3.2184L7.50005 6.68682L4.03164 3.2184C3.80708 2.99385 3.44301 2.99385 3.21846 3.2184C2.99391 3.44295 2.99391 3.80702 3.21846 4.03157L6.68688 7.49999L3.21846 10.9684C2.99391 11.193 2.99391 11.557 3.21846 11.7816C3.44301 12.0061 3.80708 12.0061 4.03164 11.7816L7.50005 8.31316L10.9685 11.7816C11.193 12.0061 11.5571 12.0061 11.7816 11.7816C12.0062 11.557 12.0062 11.193 11.7816 10.9684L8.31322 7.49999L11.7816 4.03157Z" fill="currentColor" />
          </svg>
        </Button>
      </div>

      <div className="flex-1 overflow-y-auto px-4 py-4 min-h-0">
        <div className="space-y-4">
        {/* Title */}
        <div className="space-y-1.5">
          <Label className="text-sm">{t(($) => $.node.title)}</Label>
          <Input
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder={t(($) => $.node.title_placeholder)}
            className="h-8 text-sm"
          />
        </div>

        {/* Description */}
        <div className="space-y-1.5">
          <Label className="text-sm">{t(($) => $.node.description)}</Label>
          <Textarea
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder={t(($) => $.node.description_placeholder)}
            className="min-h-[60px] text-sm"
            rows={2}
          />
        </div>

        {/* Format Schema */}
        <div className="space-y-1.5">
          <Label className="text-sm">{t(($) => $.node.format_schema_label)}</Label>
          <Textarea
            value={formatSchema}
            onChange={(e) => setFormatSchema(e.target.value)}
            placeholder="{}"
            className="min-h-[80px] text-sm font-mono"
            rows={4}
          />
          <p className="text-[11px] text-muted-foreground">{t(($) => $.node.format_schema_hint)}</p>
        </div>

        {/* Worker config */}
        <div className="space-y-3 pt-2 border-t">
          <h4 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider">
            {t(($) => $.node.section_worker)}
          </h4>

          <div className="space-y-1.5">
            <Label className="text-sm">{t(($) => $.node.worker_type_label)}</Label>
            <AssigneePicker
              assigneeType={toAssigneeType(workerType)}
              assigneeId={workerId}
              onUpdate={(u) => {
                setWorkerType(fromAssigneeType(u.assignee_type ?? null));
                setWorkerId(u.assignee_id ?? null);
              }}
              align="start"
            />
          </div>

        </div>

        {/* Critic config */}
        <div className="space-y-3 pt-2 border-t">
          <h4 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider">
            {t(($) => $.node.section_critic)}
          </h4>

          <div className="space-y-1.5">
            <Label className="text-sm">{t(($) => $.node.critic_type_label)}</Label>
            <AssigneePicker
              assigneeType={toAssigneeType(criticType)}
              assigneeId={criticId}
              onUpdate={(u) => {
                setCriticType(fromAssigneeTypeCritic(u.assignee_type ?? null));
                setCriticId(u.assignee_id ?? null);
              }}
              align="start"
            />
          </div>

          {criticType === "api" && (
            <div className="space-y-1.5">
              <Label className="text-sm">{t(($) => $.node.critic_api_url_label)}</Label>
              <Input
                value={criticApiUrl}
                onChange={(e) => setCriticApiUrl(e.target.value)}
                placeholder="https://..."
                className="h-8 text-sm"
              />
              <p className="text-[11px] text-muted-foreground">{t(($) => $.node.critic_api_url_hint)}</p>
            </div>
          )}

        </div>
        </div>
      </div>

      <div className="flex flex-col gap-2 px-4 py-3 border-t shrink-0">
        <Button
          size="sm"
          className="w-full"
          onClick={handleSave}
          disabled={updateMutation.isPending}
        >
          {updateMutation.isPending ? t(($) => $.node.saving) : t(($) => $.node.save)}
        </Button>
        <Button
          size="sm"
          variant="destructive"
          className="w-full"
          onClick={handleDelete}
          disabled={deleteMutation.isPending}
        >
          <Trash2 className="h-3.5 w-3.5 mr-1.5" />
          {deleteMutation.isPending ? t(($) => $.node.saving) : t(($) => $.node.delete)}
        </Button>
      </div>
    </div>
  );
}
