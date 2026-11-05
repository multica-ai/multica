import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { FolderOpen, Globe2, Terminal, TriangleAlert } from "lucide-react";
import { ConfirmationDialog } from "@tutti-os/ui-system";
import { useTranslation } from "../../i18n/index";
import type { WorkspaceLinkAction } from "../../actions/workspaceLinkActions";
import { observer } from "mobx-react-lite";
export const AgentFullAccessWarningDialog = observer(
  AgentFullAccessWarningDialogRender
);
const FullAccessCapabilityRow = observer(FullAccessCapabilityRowRender);

export const CODEX_FULL_ACCESS_SAFETY_URL =
  "https://deploymentsafety.openai.com/gpt-5-6";

function AgentFullAccessWarningDialogRender({
  onConfirm,
  onLinkAction,
  onOpenChange,
  open
}: {
  onConfirm: () => void;
  onLinkAction?: (action: WorkspaceLinkAction) => void;
  onOpenChange: (open: boolean) => void;
  open: boolean;
}): React.JSX.Element {
  const { t } = useTranslation();

  const onClickProp1 = usePersistCallback<
    NonNullable<React.ComponentProps<"a">["onClick"]>
  >((event) => {
    onOpenChange(false);
    if (!onLinkAction) {
      return;
    }
    event.preventDefault();
    onLinkAction({
      source: "agent-full-access-warning",
      type: "open-url",
      url: CODEX_FULL_ACCESS_SAFETY_URL
    });
  });
  return (
    <ConfirmationDialog
      cancelLabel={t("agentHost.agentGui.fullAccessWarning.cancel")}
      className="nodrag tsh-desktop-no-drag gap-4 [-webkit-app-region:no-drag] sm:max-w-[520px]"
      confirmLabel={t("agentHost.agentGui.fullAccessWarning.confirm")}
      onConfirm={onConfirm}
      onOpenChange={onOpenChange}
      open={open}
      tone="destructive"
      title={
        <span className="flex items-center gap-2.5 text-[18px]">
          <TriangleAlert
            aria-hidden="true"
            className="size-5 shrink-0 text-[var(--state-danger)]"
          />
          {t("agentHost.agentGui.fullAccessWarning.title")}
        </span>
      }
      description={t("agentHost.agentGui.fullAccessWarning.description")}
    >
      <div className="overflow-hidden rounded-[12px] bg-[var(--transparency-subtle)] px-4">
        <FullAccessCapabilityRow
          description={t(
            "agentHost.agentGui.fullAccessWarning.filesDescription"
          )}
          icon={<FolderOpen aria-hidden="true" />}
          title={t("agentHost.agentGui.fullAccessWarning.filesTitle")}
        />
        <FullAccessCapabilityRow
          description={t(
            "agentHost.agentGui.fullAccessWarning.commandsDescription"
          )}
          icon={<Terminal aria-hidden="true" />}
          title={t("agentHost.agentGui.fullAccessWarning.commandsTitle")}
        />
        <FullAccessCapabilityRow
          description={t(
            "agentHost.agentGui.fullAccessWarning.internetDescription"
          )}
          icon={<Globe2 aria-hidden="true" />}
          last
          title={t("agentHost.agentGui.fullAccessWarning.internetTitle")}
        />
      </div>
      <p className="m-0 leading-[1.45] text-[var(--text-secondary)]">
        {t("agentHost.agentGui.fullAccessWarning.riskDescription")}{" "}
        <a
          className="text-primary underline-offset-2 hover:underline"
          href={CODEX_FULL_ACCESS_SAFETY_URL}
          rel="noreferrer"
          target="_blank"
          onClick={onClickProp1}
        >
          {t("agentHost.agentGui.fullAccessWarning.learnMore")}
        </a>
      </p>
    </ConfirmationDialog>
  );
}

function FullAccessCapabilityRowRender({
  description,
  icon,
  last = false,
  title
}: {
  description: string;
  icon: React.ReactNode;
  last?: boolean;
  title: string;
}): React.JSX.Element {
  return (
    <div
      className={
        last
          ? "flex items-start gap-3 py-3"
          : "flex items-start gap-3 border-b border-[var(--border-1)] py-3"
      }
    >
      <span className="mt-0.5 flex size-7 shrink-0 items-center justify-center text-[var(--text-secondary)] [&>svg]:size-5">
        {icon}
      </span>
      <span className="min-w-0">
        <span className="block font-medium text-[var(--text-primary)]">
          {title}
        </span>
        <span className="mt-0.5 block text-[var(--text-secondary)]">
          {description}
        </span>
      </span>
    </div>
  );
}
