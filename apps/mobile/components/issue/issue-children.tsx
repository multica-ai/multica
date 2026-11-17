import { ActivityIndicator, View } from "react-native";
import { router } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { IssueRow } from "@/components/issue/issue-row";
import { childIssuesOptions } from "@/data/queries/issues";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useT } from "@/lib/i18n";

export function IssueChildren({ issueId }: { issueId: string }) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const { t } = useT("issues");
  const { data, isLoading, error, refetch } = useQuery(childIssuesOptions(wsId, issueId));

  if (!isLoading && !error && !data?.length) return null;

  return (
    <View className="border-t border-border pb-2">
      <View className="flex-row items-center gap-2 px-4 pt-4 pb-2">
        <Text className="text-xs uppercase tracking-wider text-muted-foreground font-medium">
          {t("detail.sub_issues")}
        </Text>
        {data?.length ? <Text className="text-xs text-muted-foreground">{data.length}</Text> : null}
      </View>
      {isLoading ? <ActivityIndicator className="py-4" /> : error ? (
        <View className="px-4 gap-3 py-2">
          <Text className="text-sm text-destructive">{t("errors.children_load_failed")}</Text>
          <Button variant="outline" onPress={() => refetch()}>
            <Text>{t("common:actions.retry")}</Text>
          </Button>
        </View>
      ) : data?.map((child) => (
        <IssueRow
          key={child.id}
          issue={child}
          showStatus
          showStatusLabel
          onPress={() => {
            if (wsSlug) router.push(`/${wsSlug}/issue/${child.id}`);
          }}
        />
      ))}
    </View>
  );
}
