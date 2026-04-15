import { Fragment, useMemo } from "react";
import { Linking, StyleSheet, Text, View } from "react-native";
import { useRouter } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueListOptions } from "@multica/core/issues/queries";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import type { Agent, Issue, MemberWithUser } from "@multica/core/types";
import { colors, statusColors } from "@/lib/theme";

type MentionType = "member" | "agent" | "issue" | "all";

type InlinePart =
  | { type: "text"; text: string }
  | { type: "bold"; text: string }
  | { type: "italic"; text: string }
  | { type: "code"; text: string }
  | { type: "link"; text: string; href: string }
  | { type: "mention"; mentionType: MentionType; id: string; label: string }
  | { type: "issueRef"; id: string | null; label: string };

type InlineContext = {
  issueById: Map<string, Issue>;
  issueByIdentifier: Map<string, Issue>;
  memberById: Map<string, MemberWithUser>;
  agentById: Map<string, Agent>;
};

const RAW_ISSUE_IDENTIFIER = /\b[A-Z][A-Z0-9]{1,9}-\d+\b/;

export function MarkdownText({
  content,
  returnTo,
  returnLabel,
}: {
  content: string;
  returnTo?: string;
  returnLabel?: string;
}) {
  const router = useRouter();
  const wsId = useWorkspaceId();
  const normalizedContent = useMemo(
    () => preprocessMentionShortcodes(content),
    [content],
  );
  const shouldResolveIssues =
    normalizedContent.includes("mention://issue/") ||
    RAW_ISSUE_IDENTIFIER.test(normalizedContent);
  const shouldResolveMembers = normalizedContent.includes("mention://member/");
  const shouldResolveAgents = normalizedContent.includes("mention://agent/");

  const { data: issues = [] } = useQuery({
    ...issueListOptions(wsId),
    enabled: shouldResolveIssues,
  });
  const { data: members = [] } = useQuery({
    ...memberListOptions(wsId),
    enabled: shouldResolveMembers,
  });
  const { data: agents = [] } = useQuery({
    ...agentListOptions(wsId),
    enabled: shouldResolveAgents,
  });

  const blocks = useMemo(
    () => parseBlocks(normalizedContent),
    [normalizedContent],
  );
  const inlineContext = useMemo(
    () => createInlineContext(issues, members, agents),
    [issues, members, agents],
  );

  return (
    <View style={styles.container}>
      {blocks.map((block, index) => {
        if (block.type === "code") {
          return (
            <Text key={index} style={styles.codeBlock}>
              {block.text}
            </Text>
          );
        }

        if (block.type === "space") {
          return <View key={index} style={styles.space} />;
        }

        const inline = renderInline(block.text, inlineContext, router, {
          returnTo,
          returnLabel,
        });
        switch (block.type) {
          case "h1":
            return (
              <Text key={index} style={styles.h1}>
                {inline}
              </Text>
            );
          case "h2":
            return (
              <Text key={index} style={styles.h2}>
                {inline}
              </Text>
            );
          case "quote":
            return (
              <Text key={index} style={styles.quote}>
                {inline}
              </Text>
            );
          case "list":
            return (
              <View key={index} style={styles.listRow}>
                <Text style={styles.bullet}>
                  {block.ordered ? `${block.index}.` : "•"}
                </Text>
                <Text style={styles.paragraph}>{inline}</Text>
              </View>
            );
          default:
            return (
              <Text key={index} style={styles.paragraph}>
                {inline}
              </Text>
            );
        }
      })}
    </View>
  );
}

type Block =
  | { type: "paragraph"; text: string }
  | { type: "h1"; text: string }
  | { type: "h2"; text: string }
  | { type: "quote"; text: string }
  | { type: "list"; text: string; ordered: boolean; index: number }
  | { type: "code"; text: string }
  | { type: "space" };

function parseBlocks(markdown: string): Block[] {
  const lines = markdown.replace(/\r\n/g, "\n").split("\n");
  const blocks: Block[] = [];
  let inCode = false;
  let codeLines: string[] = [];

  for (const line of lines) {
    if (line.trim().startsWith("```")) {
      if (inCode) {
        blocks.push({ type: "code", text: codeLines.join("\n") });
        codeLines = [];
        inCode = false;
      } else {
        inCode = true;
      }
      continue;
    }

    if (inCode) {
      codeLines.push(line);
      continue;
    }

    if (!line.trim()) {
      if (blocks.at(-1)?.type !== "space") blocks.push({ type: "space" });
      continue;
    }

    const h1 = line.match(/^#\s+(.+)$/);
    if (h1?.[1]) {
      blocks.push({ type: "h1", text: h1[1] });
      continue;
    }

    const h2 = line.match(/^##\s+(.+)$/);
    if (h2?.[1]) {
      blocks.push({ type: "h2", text: h2[1] });
      continue;
    }

    const quote = line.match(/^>\s?(.+)$/);
    if (quote?.[1]) {
      blocks.push({ type: "quote", text: quote[1] });
      continue;
    }

    const unordered = line.match(/^\s*[-*]\s+(.+)$/);
    if (unordered?.[1]) {
      blocks.push({
        type: "list",
        text: unordered[1],
        ordered: false,
        index: 0,
      });
      continue;
    }

    const ordered = line.match(/^\s*(\d+)\.\s+(.+)$/);
    if (ordered?.[1] && ordered[2]) {
      blocks.push({
        type: "list",
        text: ordered[2],
        ordered: true,
        index: Number(ordered[1]),
      });
      continue;
    }

    blocks.push({ type: "paragraph", text: line });
  }

  if (codeLines.length > 0) {
    blocks.push({ type: "code", text: codeLines.join("\n") });
  }
  return blocks.filter((block, index, arr) => {
    if (block.type !== "space") return true;
    return index > 0 && index < arr.length - 1;
  });
}

function parseInline(text: string): InlinePart[] {
  const parts: InlinePart[] = [];
  const pattern =
    /(`[^`]+`|\*\*[^*]+\*\*|\*[^*]+\*|\[[^\]]+\]\([^)]+\)|\b[A-Z][A-Z0-9]{1,9}-\d+\b)/g;
  let lastIndex = 0;
  let match: RegExpExecArray | null;

  while ((match = pattern.exec(text))) {
    if (match.index > lastIndex) {
      parts.push({ type: "text", text: text.slice(lastIndex, match.index) });
    }
    parts.push(parseInlineToken(match[0]));
    lastIndex = match.index + match[0].length;
  }

  if (lastIndex < text.length) {
    parts.push({ type: "text", text: text.slice(lastIndex) });
  }

  return parts;
}

function parseInlineToken(raw: string): InlinePart {
  if (raw.startsWith("`")) {
    return { type: "code", text: raw.slice(1, -1) };
  }
  if (raw.startsWith("**")) {
    return { type: "bold", text: raw.slice(2, -2) };
  }
  if (raw.startsWith("*")) {
    return { type: "italic", text: raw.slice(1, -1) };
  }
  if (raw.startsWith("[")) {
    const link = raw.match(/^\[([^\]]+)\]\(([^)]+)\)$/);
    const label = link?.[1] ?? raw;
    const href = link?.[2] ?? "";
    const mention = parseMentionHref(href, label);
    if (mention) return mention;
    const issueId = getIssueIdFromHref(href);
    if (issueId) return { type: "issueRef", id: issueId, label };
    return { type: "link", text: label, href };
  }
  return { type: "issueRef", id: null, label: raw };
}

function renderInline(
  text: string,
  context: InlineContext,
  router: ReturnType<typeof useRouter>,
  navigationContext: { returnTo?: string; returnLabel?: string },
) {
  return parseInline(text).map((part, index) => {
    switch (part.type) {
      case "bold":
        return (
          <Text key={index} style={styles.bold}>
            {part.text}
          </Text>
        );
      case "italic":
        return (
          <Text key={index} style={styles.italic}>
            {part.text}
          </Text>
        );
      case "code":
        return (
          <Text key={index} style={styles.inlineCode}>
            {part.text}
          </Text>
        );
      case "link":
        return (
          <Text
            key={index}
            style={styles.link}
            onPress={() => openUrl(part.href)}
          >
            {part.text}
          </Text>
        );
      case "mention":
        if (part.mentionType === "issue") {
          return renderIssueMention(
            part.id,
            part.label,
            context,
            router,
            navigationContext,
            index,
          );
        }
        return (
          <Text key={index} style={styles.mention}>
            {formatMentionLabel(part, context)}
          </Text>
        );
      case "issueRef":
        return renderIssueMention(
          part.id,
          part.label,
          context,
          router,
          navigationContext,
          index,
        );
      default:
        return <Fragment key={index}>{part.text}</Fragment>;
    }
  });
}

function renderIssueMention(
  id: string | null,
  label: string,
  context: InlineContext,
  router: ReturnType<typeof useRouter>,
  navigationContext: { returnTo?: string; returnLabel?: string },
  key: number,
) {
  const issue =
    (id ? context.issueById.get(id) : undefined) ??
    context.issueByIdentifier.get(cleanIssueLabel(label).toUpperCase());
  const issueId = issue?.id ?? id;
  const display = issue
    ? `${issue.identifier} ${issue.title}`
    : cleanIssueLabel(label);

  return (
    <Text
      key={key}
      style={styles.issueMention}
      onPress={
        issueId ? () => openIssue(router, issueId, navigationContext) : undefined
      }
    >
      {issue ? (
        <Text style={{ color: statusColors[issue.status] }}>● </Text>
      ) : null}
      {display}
    </Text>
  );
}

function createInlineContext(
  issues: Issue[],
  members: MemberWithUser[],
  agents: Agent[],
): InlineContext {
  return {
    issueById: new Map(issues.map((issue) => [issue.id, issue])),
    issueByIdentifier: new Map(
      issues.map((issue) => [issue.identifier.toUpperCase(), issue]),
    ),
    memberById: new Map(members.map((member) => [member.user_id, member])),
    agentById: new Map(agents.map((agent) => [agent.id, agent])),
  };
}

function parseMentionHref(
  href: string,
  label: string,
): Extract<InlinePart, { type: "mention" }> | null {
  const mentionMatch = href.match(/^mention:\/\/(member|agent|issue|all)\/(.+)$/);
  if (!mentionMatch?.[1] || !mentionMatch[2]) return null;
  return {
    type: "mention",
    mentionType: mentionMatch[1] as MentionType,
    id: decodeURIComponent(mentionMatch[2]),
    label,
  };
}

function getIssueIdFromHref(href: string): string | null {
  const mention = href.match(/^mention:\/\/issue\/(.+)$/);
  if (mention?.[1]) return decodeURIComponent(mention[1]);

  const issuePath = href.match(/\/issues\/([^/?#)]+)/);
  if (issuePath?.[1]) return decodeURIComponent(issuePath[1]);

  return null;
}

function formatMentionLabel(
  part: Extract<InlinePart, { type: "mention" }>,
  context: InlineContext,
): string {
  if (part.mentionType === "all") return "@all";
  if (part.mentionType === "member") {
    const member = context.memberById.get(part.id);
    return `@${member?.name ?? stripMentionPrefix(part.label)}`;
  }
  if (part.mentionType === "agent") {
    const agent = context.agentById.get(part.id);
    return `@${agent?.name ?? stripMentionPrefix(part.label)}`;
  }
  return cleanIssueLabel(part.label);
}

function cleanIssueLabel(label: string): string {
  return stripMentionPrefix(label).trim();
}

function stripMentionPrefix(label: string): string {
  return label.replace(/^@/, "");
}

function openIssue(
  router: ReturnType<typeof useRouter>,
  issueId: string,
  navigationContext: { returnTo?: string; returnLabel?: string },
) {
  router.push({
    pathname: "/(app)/(issues)/[id]",
    params: {
      id: issueId,
      ...(navigationContext.returnTo
        ? { returnTo: navigationContext.returnTo }
        : {}),
      ...(navigationContext.returnLabel
        ? { returnLabel: navigationContext.returnLabel }
        : {}),
    },
  });
}

function openUrl(href: string) {
  if (!href) return;
  void Linking.openURL(href);
}

function preprocessMentionShortcodes(text: string): string {
  if (!text.includes("[@ ")) return text;
  return text.replace(/\[@\s+([^\]]*)\]/g, (match, attrString: string) => {
    const attrs: Record<string, string> = {};
    const re = /(\w+)="([^"]*)"/g;
    let m: RegExpExecArray | null;
    while ((m = re.exec(attrString)) !== null) {
      if (m[1] && m[2] !== undefined) attrs[m[1]] = m[2];
    }
    const { id, label } = attrs;
    if (!id || !label) return match;
    return `[@${label}](mention://member/${id})`;
  });
}

const styles = StyleSheet.create({
  container: {
    gap: 6,
  },
  h1: {
    fontSize: 20,
    fontWeight: "700",
    color: colors.foreground,
    lineHeight: 26,
  },
  h2: {
    fontSize: 17,
    fontWeight: "700",
    color: colors.foreground,
    lineHeight: 23,
  },
  paragraph: {
    flex: 1,
    fontSize: 15,
    color: colors.foreground,
    lineHeight: 22,
  },
  bold: {
    fontWeight: "700",
  },
  italic: {
    fontStyle: "italic",
  },
  inlineCode: {
    fontFamily: "Menlo",
    fontSize: 13,
    backgroundColor: colors.secondary,
    color: colors.foreground,
  },
  codeBlock: {
    fontFamily: "Menlo",
    fontSize: 12,
    lineHeight: 18,
    color: colors.foreground,
    backgroundColor: colors.secondary,
    borderRadius: 8,
    padding: 10,
  },
  quote: {
    fontSize: 15,
    lineHeight: 22,
    color: colors.mutedForeground,
    borderLeftWidth: 3,
    borderLeftColor: colors.border,
    paddingLeft: 10,
  },
  listRow: {
    flexDirection: "row",
    gap: 8,
    alignItems: "flex-start",
  },
  bullet: {
    width: 22,
    fontSize: 15,
    lineHeight: 22,
    color: colors.mutedForeground,
    textAlign: "right",
  },
  link: {
    color: colors.info,
    fontWeight: "600",
  },
  mention: {
    color: colors.brand,
    fontWeight: "700",
  },
  issueMention: {
    color: colors.foreground,
    fontWeight: "700",
    backgroundColor: colors.secondary,
    borderRadius: 6,
  },
  space: {
    height: 4,
  },
});
