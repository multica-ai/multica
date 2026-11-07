import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { useMemo } from "react";
import {
  createContext,
  useContext,
  useEffect,
  useState,
  type ComponentPropsWithoutRef,
  type JSX,
  type MouseEvent,
  type MouseEventHandler,
  type ReactNode
} from "react";
import { NodeViewWrapper, type NodeViewProps } from "@tiptap/react";
import {
  useResolvedRichTextMention,
  useRichTextMentionService
} from "@tutti-os/ui-rich-text/editor";
import type { RichTextMentionIdentity } from "@tutti-os/ui-rich-text/types";
import { MentionPill } from "@tutti-os/ui-system/components";
import { Spinner } from "@tutti-os/ui-system";
import { CloseIcon } from "@tutti-os/ui-system/icons";
import { useTranslation } from "../../../i18n/index";
import {
  resolveAgentMentionFileThumbnailUrl,
  resolveAgentMentionFileVisualKind
} from "../../shared/mentionFilePresentation";
import { getAgentCustomMentionKind } from "../../../shared/agentCustomMentionKinds";
import { managedAgentRoundedIconUrl } from "../../../shared/managedAgentIcons";
import { useAgentTargetPresentations } from "../../../shared/AgentTargetPresentationContext";
import {
  resolveAgentMentionTargetPresentation,
  type AgentMessageMarkdownAgentTarget
} from "../../../shared/agentTargetPresentation";
import {
  parseResolvableAgentMentionIdentity,
  resolveAgentMentionNodePresentation,
  type AgentMentionResolvedPresentation
} from "./agentMentionNodeResolution";
import { AGENT_RICH_TEXT_CARET_ANCHOR } from "./agentRichTextCaretAnchor";
import { agentExternalPromptFileErrorI18nKey } from "../model/agentExternalPromptFiles";
import { dirnameFromPath } from "./agentMentionMarkdown";
import { observer } from "mobx-react-lite";
const AgentMentionNodeWrapper = observer(AgentMentionNodeWrapperRender);
const AgentMentionStaticWrapper = observer(AgentMentionStaticWrapperRender);
export const AgentMentionTooltipProviderScope = observer(
  AgentMentionTooltipProviderScopeRender
);
const AgentMentionLegacyFileView = observer(AgentMentionLegacyFileViewRender);
const AgentMentionView = observer(AgentMentionViewRender);
const AgentMentionResolvedView = observer(AgentMentionResolvedViewRender);
const AgentMentionPresentationView = observer(
  AgentMentionPresentationViewRender
);
export const AgentMentionReadonlyView = observer(
  AgentMentionReadonlyViewRender
);
export const AgentMentionNodeView = observer(AgentMentionNodeViewRender);

type AgentMentionNodeViewKind =
  | "file"
  | "workspace-member"
  | "agent-target"
  | "session"
  | "workspace-app"
  | "workspace-reference"
  | "workspace-app-factory"
  | "workspace-issue"
  | "custom";

function parseFileCountAttr(value: unknown): number {
  const parsed =
    typeof value === "string" ? Number.parseInt(value.trim(), 10) : Number.NaN;
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
}

interface AgentMentionNodeViewModel {
  attachmentStatus?: "uploading" | "ready" | "error";
  attachmentErrorLabel?: string;
  ariaLabel: string;
  /** 宿主注册的自定义 mention kind(kind === "custom" 专用)。 */
  customKind?: string;
  directoryPath: string;
  entryKind: string;
  href: string;
  iconUrl?: string;
  kind: AgentMentionNodeViewKind;
  label: string;
  summary?: string;
  thumbnailUrl?: string;
  /** 引用文件数量(workspace-reference 专用)。 */
  fileCount?: number;
}

type AgentMentionWrapperProps = ComponentPropsWithoutRef<"span">;
type AgentMentionWrapper = (props: AgentMentionWrapperProps) => JSX.Element;

function AgentMentionNodeWrapperRender(
  props: AgentMentionWrapperProps
): JSX.Element {
  return <NodeViewWrapper as="span" {...props} />;
}

function AgentMentionStaticWrapperRender(
  props: AgentMentionWrapperProps
): JSX.Element {
  return <span {...props} />;
}

const AgentMentionTooltipProviderContext = createContext(true);

function AgentMentionTooltipProviderScopeRender({
  children,
  withTooltipProvider
}: {
  children: ReactNode;
  withTooltipProvider: boolean;
}): JSX.Element {
  return (
    <AgentMentionTooltipProviderContext value={withTooltipProvider}>
      {children}
    </AgentMentionTooltipProviderContext>
  );
}

function attrString(attrs: Record<string, unknown>, key: string): string {
  const value = attrs[key];
  return typeof value === "string" ? value : "";
}

function normalizeKind(value: string): AgentMentionNodeViewKind {
  if (value === "session" || value === "agent-session") {
    return "session";
  }
  if (value === "workspace-issue") {
    return "workspace-issue";
  }
  if (value === "workspace-app") {
    return "workspace-app";
  }
  if (value === "workspace-reference") {
    return "workspace-reference";
  }
  if (value === "workspace-app-factory") {
    return "workspace-app-factory";
  }
  if (value === "workspace-member") return "workspace-member";
  if (value === "agent-target") {
    return "agent-target";
  }
  if (value === "custom") {
    return "custom";
  }
  return "file";
}

function mentionViewModel(
  attrs: Record<string, unknown>,
  t: (key: string) => string,
  agentTargets: readonly AgentMessageMarkdownAgentTarget[],
  resolvedPresentation?: AgentMentionResolvedPresentation
): AgentMentionNodeViewModel {
  const kind = normalizeKind(attrString(attrs, "kind"));
  const name = attrString(attrs, "name");
  const displayName = resolvedPresentation?.label ?? name;
  const href = attrString(attrs, "href");

  if (kind === "session") {
    const label = attrString(attrs, "title").trim() || name.trim();
    const agentTargetId = attrString(attrs, "agentTargetId").trim();
    const presentation = resolveAgentMentionTargetPresentation({
      agentTargetId,
      agentTargets,
      fallbackIconUrl: attrString(attrs, "iconUrl"),
      workspaceId: attrString(attrs, "workspaceId")
    });
    return {
      ariaLabel:
        `${t("agentHost.agentGui.mentionKindSession")} ${label}`.trim(),
      directoryPath: "",
      entryKind: "",
      href,
      iconUrl: presentation.iconUrl,
      kind,
      label
    };
  }

  if (kind === "workspace-issue") {
    return {
      ariaLabel:
        `${t("agentHost.agentGui.mentionKindIssue")} ${displayName}`.trim(),
      directoryPath: "",
      entryKind: "",
      href,
      iconUrl:
        resolvedPresentation?.iconUrl ??
        (resolvedPresentation
          ? undefined
          : attrString(attrs, "iconUrl").trim() || undefined),
      kind,
      label: displayName
    };
  }

  if (kind === "workspace-app") {
    return {
      ariaLabel:
        `${t("agentHost.agentGui.mentionKindApp")} ${displayName}`.trim(),
      directoryPath: "",
      entryKind: "",
      href,
      iconUrl:
        resolvedPresentation?.iconUrl ??
        (resolvedPresentation
          ? undefined
          : attrString(attrs, "iconUrl").trim() || undefined),
      kind,
      label: displayName
    };
  }

  if (kind === "workspace-reference") {
    return {
      ariaLabel:
        `${t("agentHost.agentGui.mentionKindApp")} ${displayName}`.trim(),
      directoryPath: "",
      entryKind: "",
      href,
      iconUrl: attrString(attrs, "iconUrl").trim() || undefined,
      kind,
      label: displayName,
      fileCount: parseFileCountAttr(attrs.fileCount)
    };
  }

  if (kind === "workspace-app-factory") {
    return {
      ariaLabel:
        `${t("agentHost.agentGui.mentionKindAppFactory")} ${displayName}`.trim(),
      directoryPath: "",
      entryKind: "",
      href,
      kind,
      label: displayName
    };
  }

  if (kind === "workspace-member") {
    return { ariaLabel: displayName, directoryPath: "", entryKind: "", href, kind, label: displayName,
      iconUrl: resolvedPresentation?.iconUrl ?? (attrString(attrs, "iconUrl").trim() || undefined) };
  }
  if (kind === "agent-target") {
    const agentProviderId =
      resolvedPresentation?.agentProviderId ??
      (resolvedPresentation ? "" : attrString(attrs, "agentProviderId").trim());
    const presentation = resolveAgentMentionTargetPresentation({
      agentTargetId: attrString(attrs, "targetId"),
      agentTargets,
      fallbackIconUrl:
        resolvedPresentation?.iconUrl ||
        (!resolvedPresentation && attrString(attrs, "iconUrl").trim()) ||
        managedAgentRoundedIconUrl(agentProviderId || undefined),
      fallbackName: displayName,
      fallbackProvider: agentProviderId,
      workspaceId: attrString(attrs, "workspaceId")
    });
    return {
      ariaLabel:
        `${t("agentHost.agentGui.mentionKindAgent")} ${presentation.name ?? displayName}`.trim(),
      directoryPath: "",
      entryKind: "",
      href,
      iconUrl: presentation.iconUrl,
      kind,
      label: presentation.name ?? displayName
    };
  }

  if (kind === "custom") {
    return {
      ariaLabel:
        `${t("agentHost.agentGui.mentionKindReference")} ${displayName}`.trim(),
      customKind: attrString(attrs, "customKind").trim(),
      directoryPath: "",
      entryKind: "",
      href,
      kind,
      label: displayName,
      summary: attrString(attrs, "preview").trim() || undefined
    };
  }

  const path = attrString(attrs, "path") || href;
  const entryKind = attrString(attrs, "entryKind") || "unknown";
  return {
    attachmentStatus:
      attrString(attrs, "attachmentStatus") === "uploading" ||
      attrString(attrs, "attachmentStatus") === "error"
        ? (attrString(attrs, "attachmentStatus") as "uploading" | "error")
        : attrString(attrs, "attachmentId")
          ? "ready"
          : undefined,
    attachmentErrorLabel:
      attrString(attrs, "attachmentStatus") === "error"
        ? t(
            agentExternalPromptFileErrorI18nKey(
              attrString(attrs, "attachmentErrorCode")
            )
          )
        : undefined,
    ariaLabel: name,
    directoryPath: attrString(attrs, "directoryPath") || dirnameFromPath(path),
    entryKind,
    href: href || path,
    kind,
    label: name,
    thumbnailUrl: resolveAgentMentionFileThumbnailUrl({
      entryKind,
      href: href || path,
      name,
      path,
      thumbnailUrl: attrString(attrs, "thumbnailUrl")
    })
  };
}

function fileVisualKind(entryKind: string, path: string): string {
  return resolveAgentMentionFileVisualKind({ entryKind, path });
}

function hasPromptContentAfterMentionRemoval(
  doc: NodeViewProps["editor"]["state"]["doc"]
): boolean {
  let hasContent = false;
  doc.descendants((node) => {
    if (hasContent) {
      return false;
    }
    if (node.type.name === "agentFileMention") {
      hasContent = true;
      return false;
    }
    if (
      node.isText &&
      node.textContent.replaceAll(AGENT_RICH_TEXT_CARET_ANCHOR, "").trim()
        .length > 0
    ) {
      hasContent = true;
      return false;
    }
    return true;
  });
  return hasContent;
}

function AgentMentionLegacyFileViewRender({
  isEditable,
  mention,
  onRemove,
  removeActionAriaLabel,
  selected,
  Wrapper
}: AgentMentionLegacyFileViewProps): JSX.Element {
  return (
    <Wrapper
      aria-label={
        mention.attachmentErrorLabel
          ? `${mention.ariaLabel}, ${mention.attachmentErrorLabel}`
          : mention.ariaLabel
      }
      className={`agent-rich-text-mention-node group tsh-agent-object-token tsh-agent-object-token--file ${
        selected ? "is-selected" : ""
      }`}
      contentEditable={false}
      data-agent-file-directory-path={mention.directoryPath}
      data-agent-file-entry-kind={mention.entryKind}
      data-agent-file-mention="true"
      {...(mention.thumbnailUrl
        ? {}
        : {
            "data-agent-file-visual-kind": fileVisualKind(
              mention.entryKind,
              mention.href || mention.label
            )
          })}
      data-agent-mention-href={mention.href}
      data-agent-mention-kind={mention.kind}
      data-uploading={
        mention.attachmentStatus === "uploading" ? "true" : undefined
      }
      data-upload-error={
        mention.attachmentStatus === "error" ? "true" : undefined
      }
      title={mention.attachmentErrorLabel ?? undefined}
      {...(mention.thumbnailUrl
        ? { "data-agent-mention-thumbnail-url": mention.thumbnailUrl }
        : {})}
    >
      {mention.thumbnailUrl ? (
        <span
          className="agent-gui-node__mention-file-thumb relative"
          data-agent-mention-file-thumb="true"
          aria-hidden={isEditable ? undefined : true}
        >
          <img
            src={mention.thumbnailUrl}
            alt=""
            className={`h-full w-full object-cover transition-opacity ${
              isEditable
                ? "group-hover:opacity-0 group-focus-within:opacity-0"
                : ""
            }`}
            decoding="async"
            loading="lazy"
            draggable={false}
          />
          {isEditable ? (
            <button
              aria-label={removeActionAriaLabel}
              className="absolute left-1/2 top-1/2 inline-flex size-5 -translate-x-1/2 -translate-y-1/2 items-center justify-center text-[var(--text-secondary)] opacity-0 transition-opacity hover:text-[var(--text-primary)] focus-visible:opacity-100 group-hover:opacity-100"
              type="button"
              onMouseDown={onRemove}
            >
              <CloseIcon className="size-3.5" />
            </button>
          ) : null}
        </span>
      ) : (
        <span
          className="relative grid size-4 shrink-0 place-items-center"
          aria-hidden={isEditable ? undefined : true}
        >
          {mention.attachmentStatus === "uploading" ? (
            <Spinner
              className={
                isEditable
                  ? "transition-opacity group-hover:opacity-0 group-focus-within:opacity-0"
                  : undefined
              }
              size={14}
              strokeWidth={2.4}
              trackColor="var(--transparency-hover)"
              testId="agent-gui-composer-file-upload-spinner"
            />
          ) : (
            <span
              className={`tsh-agent-object-token__icon transition-opacity ${
                isEditable
                  ? "group-hover:opacity-0 group-focus-within:opacity-0"
                  : ""
              }`}
              aria-hidden="true"
            />
          )}
          {isEditable ? (
            <button
              aria-label={removeActionAriaLabel}
              className="absolute left-1/2 top-1/2 inline-flex size-5 -translate-x-1/2 -translate-y-1/2 items-center justify-center text-[var(--text-secondary)] opacity-0 transition-opacity hover:text-[var(--text-primary)] focus-visible:opacity-100 group-hover:opacity-100"
              type="button"
              onMouseDown={onRemove}
            >
              <CloseIcon className="size-3.5" />
            </button>
          ) : null}
        </span>
      )}
      <span className="tsh-agent-object-token__main">{mention.label}</span>
    </Wrapper>
  );
}

interface AgentMentionViewProps {
  agentTargets?: readonly AgentMessageMarkdownAgentTarget[];
  attrs: Record<string, unknown>;
  isEditable: boolean;
  onRemove?: MouseEventHandler<HTMLButtonElement>;
  removeActionAriaLabel?: string;
  selected: boolean;
  Wrapper: AgentMentionWrapper;
}

function AgentMentionViewRender(props: AgentMentionViewProps): JSX.Element {
  const identity = parseResolvableAgentMentionIdentity(
    props.attrs,
    normalizeKind(attrString(props.attrs, "kind"))
  );
  return identity ? (
    <AgentMentionResolvedView {...props} identity={identity} />
  ) : (
    <AgentMentionPresentationView {...props} />
  );
}

function AgentMentionResolvedViewRender({
  identity,
  ...props
}: AgentMentionViewProps & {
  identity: RichTextMentionIdentity;
}): JSX.Element {
  const mentionService = useRichTextMentionService();
  const snapshot = useResolvedRichTextMention(identity);
  const resolvedPresentation = resolveAgentMentionNodePresentation({
    attrs: props.attrs,
    hasMentionService: mentionService !== null,
    resolved: snapshot.state === "ready" ? snapshot.resolved : undefined,
    state: snapshot.state
  });
  return (
    <AgentMentionPresentationView
      {...props}
      resolvedPresentation={resolvedPresentation}
    />
  );
}

function AgentMentionPresentationViewRender({
  agentTargets,
  attrs,
  isEditable,
  onRemove,
  removeActionAriaLabel,
  resolvedPresentation,
  selected,
  Wrapper
}: AgentMentionViewProps & {
  resolvedPresentation?: AgentMentionResolvedPresentation;
}): JSX.Element {
  const { t } = useTranslation();
  const withTooltipProvider = useContext(AgentMentionTooltipProviderContext);
  const contextAgentTargets = useAgentTargetPresentations();
  const mention = mentionViewModel(
    attrs,
    t,
    agentTargets ?? contextAgentTargets,
    resolvedPresentation
  );

  if (mention.kind === "file") {
    return (
      <AgentMentionLegacyFileView
        isEditable={isEditable}
        mention={mention}
        onRemove={onRemove}
        removeActionAriaLabel={removeActionAriaLabel}
        selected={selected}
        Wrapper={Wrapper}
      />
    );
  }

  if (
    mention.kind === "workspace-member" ||
    mention.kind === "workspace-app" ||
    mention.kind === "workspace-reference" ||
    mention.kind === "agent-target"
  ) {
    return (
      <Wrapper
        aria-label={mention.ariaLabel}
        className={`agent-rich-text-mention-node inline-flex max-w-[min(100%,var(--agent-mention-max-width,16rem))] align-middle ${
          selected ? "is-selected" : ""
        }`}
        contentEditable={false}
        data-agent-file-mention="true"
        data-agent-mention-href={mention.href}
        data-agent-mention-icon-url={mention.iconUrl}
        data-agent-mention-kind={mention.kind}
      >
        <MentionPillBinding1
          mention={mention}
          isEditable={isEditable}
          removeActionAriaLabel={removeActionAriaLabel}
          onRemove={onRemove}
          withTooltipProvider={withTooltipProvider}
        />
      </Wrapper>
    );
  }

  if (mention.kind === "custom") {
    // 宿主注册的自定义 mention:优先用注册的 renderChip,缺省用通用双行卡
    // (第一行 name,第二行 summary)。点击经 rich-text surface 的 link click 委托
    // (data-agent-mention-href)上抛 onLinkAction,由宿主二次解析 href。
    const definition = getAgentCustomMentionKind(mention.customKind ?? "");
    const removeAction = isEditable ? (
      <button
        aria-label={removeActionAriaLabel}
        className="inline-flex size-4 shrink-0 items-center justify-center text-[var(--text-secondary)] opacity-0 transition-opacity hover:text-[var(--text-primary)] focus-visible:opacity-100 group-hover:opacity-100"
        type="button"
        onMouseDown={onRemove}
      >
        <CloseIcon className="size-3.5" />
      </button>
    ) : null;
    if (definition?.renderChip) {
      return (
        <Wrapper
          aria-label={mention.ariaLabel}
          className={`agent-rich-text-mention-node inline-grid max-w-[min(100%,var(--agent-mention-max-width,20rem))] align-middle ${
            selected ? "is-selected" : ""
          }`}
          contentEditable={false}
          data-agent-custom-mention="true"
          data-agent-file-mention="true"
          data-agent-mention-href={mention.href}
          data-agent-mention-kind={mention.customKind || mention.kind}
        >
          {definition.renderChip({
            href: mention.href,
            name: mention.label,
            summary: mention.summary,
            isEditable,
            removeAction
          })}
        </Wrapper>
      );
    }
    return (
      <Wrapper
        aria-label={mention.ariaLabel}
        className={`agent-rich-text-mention-node inline-grid max-w-[min(100%,var(--agent-mention-max-width,20rem))] align-middle ${
          selected ? "is-selected" : ""
        }`}
        contentEditable={false}
        data-agent-custom-mention="true"
        data-agent-file-mention="true"
        data-agent-mention-href={mention.href}
        data-agent-mention-kind={mention.customKind || mention.kind}
      >
        <span
          className="group relative grid max-w-full cursor-pointer gap-0.5 overflow-hidden rounded-[8px] border border-[var(--border-primary,rgba(0,0,0,0.08))] bg-block px-2.5 py-1.5 text-left align-middle"
          data-slot="mention-card"
        >
          <span className="flex min-w-0 items-center gap-1">
            <span
              aria-hidden="true"
              className="tsh-agent-object-token__kind-icon size-3.5 shrink-0"
            />
            <span className="truncate text-[13px] font-medium leading-[130%]">
              {mention.label}
            </span>
            {removeAction}
          </span>
          {mention.summary ? (
            <span className="truncate text-[12px] font-normal leading-[130%] text-[var(--text-tertiary)]">
              {mention.summary}
            </span>
          ) : null}
        </span>
      </Wrapper>
    );
  }

  return (
    <Wrapper
      className={`agent-rich-text-mention-node inline-flex max-w-[min(100%,var(--agent-mention-max-width,16rem))] align-middle ${
        selected ? "is-selected" : ""
      }`}
      contentEditable={false}
    >
      <MentionPillBinding2
        mention={mention}
        isEditable={isEditable}
        removeActionAriaLabel={removeActionAriaLabel}
        onRemove={onRemove}
        withTooltipProvider={withTooltipProvider}
      />
    </Wrapper>
  );
}

function AgentMentionReadonlyViewRender({
  agentTargets,
  attrs
}: {
  agentTargets?: readonly AgentMessageMarkdownAgentTarget[];
  attrs: Record<string, unknown>;
}): JSX.Element {
  const View =
    normalizeKind(attrString(attrs, "kind")) === "workspace-member"
      ? AgentMentionView
      : AgentMentionPresentationView;
  return (
    <View
      agentTargets={agentTargets}
      attrs={attrs}
      isEditable={false}
      selected={false}
      Wrapper={AgentMentionStaticWrapper}
    />
  );
}

function AgentMentionNodeViewRender({
  deleteNode,
  editor,
  extension,
  node,
  selected
}: NodeViewProps): JSX.Element {
  const [isEditable, setIsEditable] = useState(editor.isEditable);
  const extensionOptions = extension.options as {
    removeActionAriaLabel?: string;
  };
  const removeActionAriaLabel =
    typeof extensionOptions.removeActionAriaLabel === "string"
      ? extensionOptions.removeActionAriaLabel
      : undefined;

  useEffect(() => {
    const syncEditable = () => {
      setIsEditable(editor.isEditable);
    };

    syncEditable();
    editor.on("transaction", syncEditable);
    editor.on("update", syncEditable);
    return () => {
      editor.off("transaction", syncEditable);
      editor.off("update", syncEditable);
    };
  }, [editor]);

  const handleRemove = (event: MouseEvent<HTMLButtonElement>) => {
    event.preventDefault();
    event.stopPropagation();
    if (!editor.isEditable) {
      return;
    }
    deleteNode();
    if (!hasPromptContentAfterMentionRemoval(editor.state.doc)) {
      editor.commands.clearContent();
    }
  };
  const handleRemoveCallback = usePersistCallback(handleRemove);

  const attrsProp1 = useMemo<
    NonNullable<React.ComponentProps<typeof AgentMentionView>["attrs"]>
  >(() => ({}), []);
  return (
    <AgentMentionView
      attrs={node.attrs ?? attrsProp1}
      isEditable={isEditable}
      onRemove={handleRemoveCallback}
      removeActionAriaLabel={removeActionAriaLabel}
      selected={selected}
      Wrapper={AgentMentionNodeWrapper}
    />
  );
}

const MentionPillBinding1 = observer(function MentionPillBinding1({
  mention,
  isEditable,
  removeActionAriaLabel,
  onRemove,
  withTooltipProvider
}: MentionPillBinding1Props) {
  const usesSessionIcon = mention.kind === "agent-target" || mention.kind === "workspace-member";
  const iconContainerPropsProp1Value1 =
    usesSessionIcon ? undefined : "true";
  const iconContainerPropsProp1Value2 =
    usesSessionIcon ? "true" : undefined;
  const iconContainerPropsProp1Value3 =
    usesSessionIcon ? undefined : "true";
  const iconContainerPropsProp1 = useMemo<
    NonNullable<React.ComponentProps<typeof MentionPill>["iconContainerProps"]>
  >(
    () => ({
      "data-agent-mention-app-icon": iconContainerPropsProp1Value1,
      "data-agent-mention-session-icon": iconContainerPropsProp1Value2,
      "data-workspace-app-icon": iconContainerPropsProp1Value3
    }),
    [
      iconContainerPropsProp1Value1,
      iconContainerPropsProp1Value2,
      iconContainerPropsProp1Value3
    ]
  );

  const removeButtonPropsProp2 = useMemo<
    NonNullable<React.ComponentProps<typeof MentionPill>["removeButtonProps"]>
  >(
    () => ({ "aria-label": removeActionAriaLabel, onMouseDown: onRemove }),
    [removeActionAriaLabel, onRemove]
  );
  return (
    <MentionPill
      aria-label={mention.ariaLabel}
      className="top-0 h-6 max-w-[min(100%,var(--agent-mention-max-width,16rem))] py-0 align-middle leading-6"
      data-agent-mention-kind={mention.kind}
      iconUrl={mention.iconUrl}
      iconContainerProps={iconContainerPropsProp1}
      kind={usesSessionIcon ? "session" : "app"}
      label={mention.label}
      removable={isEditable}
      removeButtonProps={isEditable ? removeButtonPropsProp2 : undefined}
      withTooltipProvider={withTooltipProvider}
    />
  );
});

const MentionPillBinding2 = observer(function MentionPillBinding2({
  mention,
  isEditable,
  removeActionAriaLabel,
  onRemove,
  withTooltipProvider
}: MentionPillBinding2Props) {
  const removeButtonPropsProp3 = useMemo<
    NonNullable<React.ComponentProps<typeof MentionPill>["removeButtonProps"]>
  >(
    () => ({ "aria-label": removeActionAriaLabel, onMouseDown: onRemove }),
    [removeActionAriaLabel, onRemove]
  );
  return (
    <MentionPill
      aria-label={mention.ariaLabel}
      className="top-0 h-6 py-0 align-middle leading-6"
      data-agent-mention-href={mention.href}
      data-agent-mention-kind={mention.kind}
      kind={mention.kind === "session" ? "session" : "issue"}
      iconUrl={mention.iconUrl}
      label={mention.label}
      removable={isEditable}
      removeButtonProps={isEditable ? removeButtonPropsProp3 : undefined}
      summary={mention.summary}
      withTooltipProvider={withTooltipProvider}
    />
  );
});

type AgentMentionLegacyFileViewProps = {
  isEditable: boolean;
  mention: AgentMentionNodeViewModel;
  onRemove?: MouseEventHandler<HTMLButtonElement>;
  removeActionAriaLabel?: string;
  selected: boolean;
  Wrapper: AgentMentionWrapper;
};

type MentionPillBinding1Props = {
  mention: AgentMentionNodeViewModel;
  isEditable: NonNullable<
    AgentMentionViewProps & {
      resolvedPresentation?: AgentMentionResolvedPresentation;
    }
  >["isEditable"];
  removeActionAriaLabel: NonNullable<
    AgentMentionViewProps & {
      resolvedPresentation?: AgentMentionResolvedPresentation;
    }
  >["removeActionAriaLabel"];
  onRemove: NonNullable<
    AgentMentionViewProps & {
      resolvedPresentation?: AgentMentionResolvedPresentation;
    }
  >["onRemove"];
  withTooltipProvider: boolean;
};

type MentionPillBinding2Props = {
  mention: AgentMentionNodeViewModel;
  isEditable: boolean;
  removeActionAriaLabel: string | undefined;
  onRemove: MouseEventHandler<HTMLButtonElement> | undefined;
  withTooltipProvider: boolean;
};
