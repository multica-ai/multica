/**
 * Slim header for the issue detail screen.
 *
 * Linear iOS-inspired layout:
 *   - identifier (MUL-NN) above as a small muted label
 *   - title in a large bold treatment
 *   - attribute chip row below (status / priority / assignee / labels /
 *     project / due date) — tappable, opens picker sheets
 *
 * The native iOS Stack header still renders `issue.identifier` as the
 * navigation title; the body re-renders it more prominently per the
 * reference screenshot.
 */
import { View } from "react-native";
import type { Issue } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { AttributeRow } from "./attribute-row";
import { AgentActivityRow } from "./agent-activity-row";

export function IssueHeaderCard({ issue }: { issue: Issue }) {
  return (
    <View className="px-4 pt-4 pb-3 gap-3">
      <Text className="text-xs text-muted-foreground">{issue.identifier}</Text>
      <Text className="text-2xl font-bold text-foreground">
        {issue.title}
      </Text>
      {/* Activity row sits between title and attributes — it represents
       *  "who's doing this issue right now / who has done it" (dynamic),
       *  which is higher-IA than the static property chips below.
       *  Conditionally renders null when there are no tasks at all. */}
      <AgentActivityRow issueId={issue.id} />
      <AttributeRow issue={issue} />
    </View>
  );
}
