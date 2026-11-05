import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { useMemo } from "react";
import {
  Children,
  isValidElement,
  useCallback,
  useRef,
  useState,
  type CSSProperties,
  type JSX,
  type ReactNode
} from "react";
import { Check, Copy } from "lucide-react";
import { translate } from "../i18n/index";
import { cn } from "../app/renderer/lib/utils";
import type { MarkdownDomProps } from "./AgentMessageMarkdown";
import { AgentMessageMermaid } from "./AgentMessageMermaid";
import { observer } from "mobx-react-lite";
export const MarkdownCode = observer(MarkdownCodeRender);
export const MarkdownUnorderedList = observer(MarkdownUnorderedListRender);
export const MarkdownOrderedList = observer(MarkdownOrderedListRender);
export const MarkdownListItem = observer(MarkdownListItemRender);
export const MarkdownParagraph = observer(MarkdownParagraphRender);
export const MarkdownPre = observer(MarkdownPreRender);

function MarkdownCodeRender({
  node: _node,
  children,
  className,
  ...props
}: MarkdownDomProps<"code">): JSX.Element {
  "use memo";
  return (
    <code {...props} className={className}>
      {children}
    </code>
  );
}

const MARKDOWN_ORDERED_LIST_STYLE: CSSProperties = {
  listStylePosition: "outside",
  margin: "12px 0 8px",
  paddingInlineStart: 34,
  paddingInlineEnd: 16
};

const MARKDOWN_UNORDERED_LIST_STYLE: CSSProperties = {
  margin: "12px 0 8px",
  paddingInlineStart: 0
};

const MARKDOWN_LIST_ITEM_STYLE: CSSProperties = {
  margin: "4px 0"
};

function MarkdownUnorderedListRender({
  node: _node,
  className,
  style,
  ...props
}: MarkdownDomProps<"ul">): JSX.Element {
  "use memo";

  const styleProp1 = useMemo<NonNullable<React.ComponentProps<"ul">["style"]>>(
    () => ({ ...MARKDOWN_UNORDERED_LIST_STYLE, ...style }),
    [MARKDOWN_UNORDERED_LIST_STYLE, style]
  );
  return (
    <ul
      {...props}
      className={cn(
        '[&_li]:relative [&_li]:list-none [&_li]:pl-[34px] [&_li::before]:absolute [&_li::before]:left-4 [&_li::before]:top-[0.78em] [&_li::before]:h-1.5 [&_li::before]:w-1.5 [&_li::before]:-translate-y-1/2 [&_li::before]:rounded-full [&_li::before]:bg-[var(--text-tertiary)] [&_li::before]:content-[""]',
        className
      )}
      style={styleProp1}
    />
  );
}

function MarkdownOrderedListRender({
  node: _node,
  style,
  ...props
}: MarkdownDomProps<"ol">): JSX.Element {
  "use memo";

  const styleProp2 = useMemo<NonNullable<React.ComponentProps<"ol">["style"]>>(
    () => ({
      ...MARKDOWN_ORDERED_LIST_STYLE,
      listStyleType: "decimal",
      ...style
    }),
    [MARKDOWN_ORDERED_LIST_STYLE, style]
  );
  return <ol {...props} style={styleProp2} />;
}

function MarkdownListItemRender({
  node: _node,
  style,
  ...props
}: MarkdownDomProps<"li">): JSX.Element {
  "use memo";

  const styleProp3 = useMemo<NonNullable<React.ComponentProps<"li">["style"]>>(
    () => ({ ...MARKDOWN_LIST_ITEM_STYLE, ...style }),
    [MARKDOWN_LIST_ITEM_STYLE, style]
  );
  return <li {...props} style={styleProp3} />;
}

function MarkdownParagraphRender({
  node: _node,
  inline,
  ...props
}: MarkdownDomProps<"p"> & {
  inline: boolean;
}): JSX.Element {
  "use memo";
  if (inline) {
    return <span {...props} />;
  }
  return <p {...props} />;
}

export function textFromReactNode(node: ReactNode): string {
  if (typeof node === "string" || typeof node === "number") {
    return String(node);
  }
  if (Array.isArray(node)) {
    return node.map(textFromReactNode).join("");
  }
  return "";
}

function MarkdownPreRender({
  children,
  mermaidEnabled = true,
  node: _node,
  streaming = false,
  ...props
}: MarkdownDomProps<"pre"> & {
  mermaidEnabled?: boolean;
  streaming?: boolean;
}): JSX.Element {
  "use memo";
  const preRef = useRef<HTMLPreElement>(null);
  const [copied, setCopied] = useState(false);
  const copyResetRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const mermaidSource = mermaidEnabled
    ? resolveMermaidCodeBlockSource(children)
    : null;
  const streamingMermaidSource =
    mermaidEnabled && streaming
      ? resolveStreamingMermaidCodeBlockSource(children)
      : null;
  const effectiveMermaidSource = mermaidSource ?? streamingMermaidSource;

  const handleCopy = useCallback(() => {
    const text = preRef.current?.textContent?.trim();
    if (!text) {
      return;
    }
    void navigator.clipboard?.writeText(text).then(() => {
      setCopied(true);
      if (copyResetRef.current) {
        clearTimeout(copyResetRef.current);
      }
      // timing: revert the "copied" affordance after the confirmation window
      copyResetRef.current = setTimeout(() => setCopied(false), 1500);
    });
  }, []);
  const handleCopyCallback = usePersistCallback(handleCopy);

  const codeBlock = (
    <div className="group relative">
      <button
        type="button"
        data-testid="markdown-code-copy"
        className="absolute right-1.5 top-1.5 z-10 inline-flex size-5 items-center justify-center rounded-[4px] text-[var(--text-tertiary)] opacity-0 transition-opacity hover:bg-[var(--transparency-hover)] hover:text-[var(--text-secondary)] group-hover:opacity-100"
        aria-label={translate("agentHost.agentGui.copyCode")}
        title={translate("agentHost.agentGui.copyCode")}
        onClick={handleCopyCallback}
      >
        {copied ? (
          <Check size={13} strokeWidth={2} aria-hidden="true" />
        ) : (
          <Copy size={13} strokeWidth={2} aria-hidden="true" />
        )}
      </button>
      <pre {...props} ref={preRef}>
        {children}
      </pre>
    </div>
  );

  if (effectiveMermaidSource === null) {
    return codeBlock;
  }

  return (
    <AgentMessageMermaid
      source={effectiveMermaidSource}
      streaming={streaming}
    />
  );
}

export function resolveMermaidCodeBlockSource(
  children: ReactNode
): string | null {
  return resolveMermaidCodeBlockSourceWithLanguage(children, (language) => {
    return language === "mermaid";
  });
}

function resolveStreamingMermaidCodeBlockSource(
  children: ReactNode
): string | null {
  return resolveMermaidCodeBlockSourceWithLanguage(children, (language) => {
    return (
      language.length > 0 &&
      language.length < "mermaid".length &&
      "mermaid".startsWith(language)
    );
  });
}

function resolveMermaidCodeBlockSourceWithLanguage(
  children: ReactNode,
  acceptsLanguage: (language: string) => boolean
): string | null {
  const childElements = Children.toArray(children);
  if (childElements.length !== 1 || !isValidElement(childElements[0])) {
    return null;
  }
  const codeElement = childElements[0] as {
    props: {
      children?: ReactNode;
      className?: unknown;
    };
  };
  const className =
    typeof codeElement.props.className === "string"
      ? codeElement.props.className
      : "";
  const languageClass = className
    .split(/\s+/)
    .find((candidate) => candidate.toLowerCase().startsWith("language-"));
  const language = languageClass?.slice("language-".length).toLowerCase() ?? "";
  if (!acceptsLanguage(language)) {
    return null;
  }
  return textFromReactNode(codeElement.props.children).replace(/\n$/, "");
}
