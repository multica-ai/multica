/**
 * HarmonyOS port of apps/mobile/components/issue/issue-description.tsx.
 * Description block rendered through the ported markdown renderer
 * (lib/markdown/). Empty / null descriptions show a muted "No description."
 * placeholder rather than collapsing the block, so the layout above the
 * timeline stays stable when the user adds a description later.
 *
 * Attachments are fetched per-issue so markdown can resolve `mc://file/<id>`
 * image URIs into real `download_url` HTTPS endpoints. TanStack Query
 * dedupes the request across this component and CommentCard.
 */
import React from "react";
import { StyleSheet, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import { Text } from "@/components/ui/text";
import { Markdown } from "@/lib/markdown/markdown";
import { issueAttachmentsOptions } from "@/data/queries/issues";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useThemeColors } from "@/lib/use-theme-colors";

export function IssueDescription({
  issueId,
  description,
}: {
  issueId: string;
  description: string | null;
}) {
  const c = useThemeColors();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: attachments } = useQuery(
    issueAttachmentsOptions(wsId, issueId),
  );

  if (!description || description.trim().length === 0) {
    return (
      <View style={styles.wrap}>
        <Text style={[styles.placeholder, { color: c.mutedForeground }]}>
          No description.
        </Text>
      </View>
    );
  }
  return (
    <View style={styles.wrap}>
      <Markdown content={description} attachments={attachments} />
    </View>
  );
}

// px-4 pb-4
const styles = StyleSheet.create({
  wrap: { paddingHorizontal: 16, paddingBottom: 16 },
  // text-sm italic
  placeholder: { fontSize: 14, fontStyle: "italic" },
});
