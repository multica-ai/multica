import { MentionPill } from "@tutti-os/ui-system/components";
import { createContext,useContext,useEffect,type ReactNode } from "react";
import { previewResourceHref } from "./message-resource-links";
import type { AgentGuiHostServices,AgentGuiWorkspaceState } from "./protocol";
import {
resolveResourceReference,
type ResourceLinkScope
} from "./resource-reference";
import { useCanPreviewResources } from "./side-entry";
import { isTaskCreationMentionHref } from "./task-creation-mention";
export { remarkMessageResourceLinks } from "./message-resource-links";

interface ResourceLinks {
  client: AgentGuiHostServices;
  scope: ResourceLinkScope;
  agents: AgentGuiWorkspaceState["agents"];
}
const ResourceLinkContext = createContext<ResourceLinks | null>(null);

export function ResourceLinkProvider({
  children,
  ...context
}: ResourceLinks & { children: ReactNode }) {
  const { client, scope } = context;
  const { workspaceId, workspaceSlug, appOrigin } = scope;
  useEffect(() => {
    let pointerActivated: string | null = null;
    const open = (event: MouseEvent | KeyboardEvent) => {
      if (
        event instanceof KeyboardEvent &&
        event.key !== "Enter" &&
        event.key !== " "
      )
        return;
      if (event.type === "pointerdown" && (event as MouseEvent).button !== 0)
        return;
      if (!(event.target instanceof Element)) return;
      const element = event.target.closest(
        "a[href],[data-agent-mention-href],[data-agent-link-href]"
      );
      const href =
        element?.getAttribute("data-multica-resource-path") ||
        element?.getAttribute("data-agent-mention-href") ||
        element?.getAttribute("data-agent-link-href") ||
        element?.getAttribute("href");
      if (href && isTaskCreationMentionHref(href)) {
        event.preventDefault();
        event.stopPropagation();
        element?.closest<HTMLElement>('[contenteditable="true"]')?.focus();
        return;
      }
      if (
        !href ||
        !resolveResourceReference(href, {
          workspaceId,
          workspaceSlug,
          appOrigin
        })
      )
        return;
      event.preventDefault();
      event.stopPropagation();
      if (
        event.type === "click" &&
        (event as MouseEvent).detail > 0 &&
        pointerActivated === href
      ) {
        pointerActivated = null;
        return;
      }
      pointerActivated = event.type === "pointerdown" ? href : null;
      void client.hostApi
        .openInternalResource({
          href,
          background: event.metaKey || event.ctrlKey
        })
        .catch((error: unknown) => {
          void client.hostApi
            .showMessage({
              message: error instanceof Error ? error.message : String(error),
              tone: "error"
            })
            .catch(() => undefined);
        });
    };
    document.addEventListener("pointerdown", open, true);
    document.addEventListener("click", open, true);
    document.addEventListener("keydown", open, true);
    return () => {
      document.removeEventListener("pointerdown", open, true);
      document.removeEventListener("click", open, true);
      document.removeEventListener("keydown", open, true);
    };
  }, [client, workspaceId, workspaceSlug, appOrigin]);
  return (
    <ResourceLinkContext.Provider value={context}>
      {children}
    </ResourceLinkContext.Provider>
  );
}

/** Upgrade authored links at the anchor boundary; code blocks and external links stay intact. */
export function ResourceMarkdownLink({
  href,
  children,
  fallback
}: {
  href?: string;
  children?: ReactNode;
  fallback: ReactNode;
}) {
  const context = useContext(ResourceLinkContext);
  const previewEnabled = useCanPreviewResources();
  const resource =
    context && href ? resolveResourceReference(href, context.scope) : null;
  if (!context || !resource) {
    const preview = previewEnabled && href ? previewResourceHref(href) : null;
    return preview ? (
      <a href={preview} data-multica-preview-link="true">
        {children}
      </a>
    ) : (
      <>{fallback}</>
    );
  }
  const agent =
    resource.kind === "agent"
      ? context.agents.find((entry) => entry.agent.id === resource.id)
      : undefined;
  const label = agent?.agent.name || children || resource.id;
  return (
    <a
      href={resource.path}
      data-agent-link-href={resource.mentionHref}
      data-agent-mention-href={resource.mentionHref}
      data-multica-resource-kind={resource.kind}
      data-multica-resource-path={resource.path}
      className="tsh-agent-object-token tsh-agent-object-token--entity"
      title={typeof label === "string" ? label : undefined}
    >
      <MentionPill
        kind={
          resource.kind === "issue"
            ? "issue"
            : resource.kind === "agent"
              ? "session"
              : "file"
        }
        fileKind={resource.kind === "project" ? "folder" : "file"}
        iconUrl={agent?.iconUrl}
        label={label}
        className="top-0 max-w-full cursor-pointer text-brand"
        style={{ color: "var(--brand)" }}
        removable={false}
      />
    </a>
  );
}
