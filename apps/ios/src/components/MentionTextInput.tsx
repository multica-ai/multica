import { useEffect, useMemo, useState } from "react";
import {
  StyleSheet,
  Text,
  TextInput,
  TouchableOpacity,
  View,
  type TextInputProps,
  type NativeSyntheticEvent,
  type TextInputSelectionChangeEventData,
} from "react-native";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueListOptions } from "@multica/core/issues/queries";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import type { Issue } from "@multica/core/types";
import { Avatar } from "@/components/Avatar";
import { colors, statusColors } from "@/lib/theme";

type MentionKind = "all" | "member" | "agent" | "issue";

type MentionCandidate = {
  key: string;
  kind: MentionKind;
  id: string;
  label: string;
  subtitle?: string;
  issue?: Issue;
};

type MentionRange = {
  start: number;
  end: number;
  query: string;
};

type Props = Omit<
  TextInputProps,
  "value" | "onChangeText" | "onSelectionChange" | "selection"
> & {
  value: string;
  onChangeText: (value: string) => void;
};

export function MentionTextInput({ value, onChangeText, ...props }: Props) {
  const wsId = useWorkspaceId();
  const [selection, setSelection] = useState({ start: value.length, end: value.length });
  const [forcedSelection, setForcedSelection] = useState<
    { start: number; end: number } | undefined
  >();
  const mentionRange = useMemo(
    () => getActiveMentionRange(value, selection.start),
    [selection.start, value],
  );

  useEffect(() => {
    if (selection.start <= value.length && selection.end <= value.length) return;
    setSelection({ start: value.length, end: value.length });
  }, [selection.end, selection.start, value.length]);

  const { data: members = [] } = useQuery({
    ...memberListOptions(wsId),
    enabled: mentionRange !== null,
  });
  const { data: agents = [] } = useQuery({
    ...agentListOptions(wsId),
    enabled: mentionRange !== null,
  });
  const { data: issues = [] } = useQuery({
    ...issueListOptions(wsId),
    enabled: mentionRange !== null,
  });

  const candidates = useMemo(() => {
    if (!mentionRange) return [];
    const q = mentionRange.query.toLowerCase();
    const allItem: MentionCandidate[] =
      "all".includes(q) || "all members".includes(q)
        ? [
            {
              key: "all",
              kind: "all",
              id: "all",
              label: "all",
              subtitle: "Notify everyone in this workspace",
            },
          ]
        : [];
    const memberItems: MentionCandidate[] = members
      .filter((member) => member.name.toLowerCase().includes(q))
      .map((member) => ({
        key: `member-${member.user_id}`,
        kind: "member",
        id: member.user_id,
        label: member.name,
        subtitle: member.email,
      }));
    const agentItems: MentionCandidate[] = agents
      .filter(
        (agent) => !agent.archived_at && agent.name.toLowerCase().includes(q),
      )
      .map((agent) => ({
        key: `agent-${agent.id}`,
        kind: "agent",
        id: agent.id,
        label: agent.name,
        subtitle: `${agent.runtime_mode} runtime`,
      }));
    const issueItems: MentionCandidate[] = issues
      .filter(
        (issue) =>
          issue.identifier.toLowerCase().includes(q) ||
          issue.title.toLowerCase().includes(q),
      )
      .map((issue) => ({
        key: `issue-${issue.id}`,
        kind: "issue",
        id: issue.id,
        label: issue.identifier,
        subtitle: issue.title,
        issue,
      }));
    return [...allItem, ...memberItems, ...agentItems, ...issueItems].slice(0, 8);
  }, [agents, issues, members, mentionRange]);

  const handleSelectionChange = (
    event: NativeSyntheticEvent<TextInputSelectionChangeEventData>,
  ) => {
    setSelection(event.nativeEvent.selection);
  };

  const insertMention = (candidate: MentionCandidate) => {
    if (!mentionRange) return;
    const mention = serializeMention(candidate);
    const next =
      value.slice(0, mentionRange.start) +
      mention +
      " " +
      value.slice(mentionRange.end);
    const nextCursor = mentionRange.start + mention.length + 1;
    onChangeText(next);
    setSelection({ start: nextCursor, end: nextCursor });
    setForcedSelection({ start: nextCursor, end: nextCursor });
    setTimeout(() => setForcedSelection(undefined), 0);
  };

  return (
    <View style={styles.container}>
      {candidates.length > 0 ? (
        <View style={styles.suggestions}>
          {candidates.map((candidate) => (
            <TouchableOpacity
              key={candidate.key}
              style={styles.suggestion}
              activeOpacity={0.7}
              onPress={() => insertMention(candidate)}
            >
              <MentionIcon candidate={candidate} />
              <View style={styles.suggestionText}>
                <Text style={styles.suggestionTitle} numberOfLines={1}>
                  {candidate.kind === "issue"
                    ? candidate.label
                    : `@${candidate.label}`}
                </Text>
                {candidate.subtitle ? (
                  <Text style={styles.suggestionSubtitle} numberOfLines={1}>
                    {candidate.subtitle}
                  </Text>
                ) : null}
              </View>
            </TouchableOpacity>
          ))}
        </View>
      ) : null}
      <TextInput
        {...props}
        value={value}
        onChangeText={onChangeText}
        onSelectionChange={handleSelectionChange}
        selection={forcedSelection}
      />
    </View>
  );
}

function MentionIcon({ candidate }: { candidate: MentionCandidate }) {
  if (candidate.kind === "member") {
    return <Avatar name={candidate.label} size={26} />;
  }
  if (candidate.kind === "agent") {
    return <Avatar name={candidate.label} size={26} isAgent />;
  }
  if (candidate.kind === "issue" && candidate.issue) {
    return (
      <View style={styles.issueIcon}>
        <View
          style={[
            styles.issueDot,
            { backgroundColor: statusColors[candidate.issue.status] },
          ]}
        />
      </View>
    );
  }
  return (
    <View style={styles.allIcon}>
      <Text style={styles.allIconText}>@</Text>
    </View>
  );
}

function getActiveMentionRange(value: string, cursor: number): MentionRange | null {
  if (cursor < 1) return null;
  const before = value.slice(0, cursor);
  const atIndex = before.lastIndexOf("@");
  if (atIndex < 0) return null;
  const prefix = atIndex === 0 ? "" : before[atIndex - 1];
  if (prefix && !/\s/.test(prefix)) return null;
  const query = before.slice(atIndex + 1);
  if (/\s/.test(query)) return null;
  return { start: atIndex, end: cursor, query };
}

function serializeMention(candidate: MentionCandidate): string {
  const label = sanitizeMentionLabel(candidate.label);
  switch (candidate.kind) {
    case "all":
      return "[@all](mention://all/all)";
    case "member":
      return `[@${label}](mention://member/${candidate.id})`;
    case "agent":
      return `[@${label}](mention://agent/${candidate.id})`;
    case "issue":
      return `[${label}](mention://issue/${candidate.id})`;
  }
}

function sanitizeMentionLabel(label: string): string {
  return label.replace(/[[\]\n\r]/g, " ").trim();
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
  },
  suggestions: {
    marginBottom: 8,
    borderRadius: 8,
    borderWidth: 1,
    borderColor: colors.border,
    backgroundColor: colors.background,
    overflow: "hidden",
  },
  suggestion: {
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
    paddingHorizontal: 12,
    paddingVertical: 9,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: colors.border,
  },
  suggestionText: {
    flex: 1,
  },
  suggestionTitle: {
    fontSize: 14,
    color: colors.foreground,
    fontWeight: "600",
  },
  suggestionSubtitle: {
    marginTop: 1,
    fontSize: 12,
    color: colors.mutedForeground,
  },
  allIcon: {
    width: 26,
    height: 26,
    borderRadius: 8,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: colors.secondary,
  },
  allIconText: {
    fontSize: 15,
    color: colors.foreground,
    fontWeight: "700",
  },
  issueIcon: {
    width: 26,
    height: 26,
    borderRadius: 8,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: colors.secondary,
  },
  issueDot: {
    width: 9,
    height: 9,
    borderRadius: 5,
  },
});
