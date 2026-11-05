import {
isLocalPermissionMode,
type LocalPermissionMode
} from "@multica/core/local-conversation/protocol";
import { Button } from "@multica/ui/components/ui/button";
import {
DropdownMenu,
DropdownMenuContent,
DropdownMenuGroup,
DropdownMenuLabel,
DropdownMenuRadioGroup,
DropdownMenuRadioItem,
DropdownMenuTrigger
} from "@multica/ui/components/ui/dropdown-menu";
import { ChevronDown,ShieldCheck } from "lucide-react";
import { useId,useState,useSyncExternalStore } from "react";
import type { AgentGuiHostServices } from "./protocol";
import type { createMulticaAgentGuiRuntime } from "./runtime";

export function PermissionPicker({
  handle,
  client,
  agentId,
  sessionId,
  chinese,
  disabled
}: {
  handle: ReturnType<typeof createMulticaAgentGuiRuntime>;
  client: AgentGuiHostServices;
  agentId: string;
  sessionId: string | null;
  chinese: boolean;
  disabled: boolean;
}) {
  useSyncExternalStore(
    handle.subscribePermissions,
    handle.getPermissionRevision
  );
  const valueId = useId();
  const [saving, setSaving] = useState(false);
  const mode = handle.getPermissionSelection(agentId, sessionId);
  const labels = chinese
    ? {
        title: "执行权限",
        running: "执行或提交期间无法切换权限",
        saving: "正在保存权限…",
        default: "默认权限",
        auto: "自动（Auto）",
        "full-access": "完全访问",
        defaultHelp: "沿用本机 CLI 配置；需要手动批准的操作可能被阻止。",
        autoHelp: "由运行器自动判断审批，仍可能拒绝操作。",
        fullHelp: "不再询问权限；可执行命令并访问工作目录之外的文件。",
        scope:
          "记住此运行器的选择，新会话默认沿用；已有会话单独保存。@ 智能体使用自身权限。"
      }
    : {
        title: "Execution permissions",
        running: "Wait for the run or submission to finish",
        saving: "Saving permissions…",
        default: "Default permissions",
        auto: "Auto",
        "full-access": "Full access",
        defaultHelp:
          "Use local CLI settings. Operations requiring manual approval may be blocked.",
        autoHelp:
          "The provider reviews approvals automatically and may still refuse operations.",
        fullHelp:
          "Skip permission prompts. Commands can access files outside the working directory.",
        scope:
          "Remembered for new conversations with this provider. Existing conversations keep their permissions. Mentioned agents use their own permissions."
      };
  const choices: { value: LocalPermissionMode; help: string }[] = [
    { value: "default", help: labels.defaultHelp },
    { value: "auto", help: labels.autoHelp },
    { value: "full-access", help: labels.fullHelp }
  ];
  const select = async (value: unknown) => {
    if (disabled || saving || !isLocalPermissionMode(value) || value === mode)
      return;
    setSaving(true);
    try {
      await handle.setPermissionSelection(agentId, sessionId, value);
    } catch (error) {
      void client.hostApi
        .showMessage({
          message: error instanceof Error ? error.message : String(error),
          tone: "error"
        })
        .catch(() => undefined);
    } finally {
      setSaving(false);
    }
  };
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant="ghost"
            size="sm"
            className="multica-permission-picker"
            data-testid="multica-permission-picker"
            aria-label={labels.title}
            aria-describedby={valueId}
            disabled={disabled || saving}
            aria-busy={saving || undefined}
            title={
              saving ? labels.saving : disabled ? labels.running : labels[mode]
            }
          />
        }
      >
        <ShieldCheck className="size-4" aria-hidden="true" />
        <span id={valueId} className="multica-composer-footer-control-label">
          {labels[mode]}
        </span>
        <ChevronDown className="size-3.5" aria-hidden="true" />
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align="start"
        side="top"
        className="nodrag w-80 max-w-[calc(100vw-24px)]"
      >
        <DropdownMenuGroup>
          <DropdownMenuLabel>{labels.title}</DropdownMenuLabel>
          <DropdownMenuRadioGroup
            value={mode}
            onValueChange={(value) => {
              void select(value);
            }}
          >
            {choices.map((choice) => (
              <DropdownMenuRadioItem
                key={choice.value}
                value={choice.value}
                disabled={disabled || saving}
                className="items-start py-2"
              >
                <span className="flex flex-col gap-1">
                  <span>{labels[choice.value]}</span>
                  <span className="text-caption font-normal text-muted-foreground">
                    {choice.help}
                  </span>
                </span>
              </DropdownMenuRadioItem>
            ))}
          </DropdownMenuRadioGroup>
        </DropdownMenuGroup>
        <p className="px-2 py-2 text-caption text-muted-foreground">
          {labels.scope}
        </p>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
