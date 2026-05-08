import { useLocalSearchParams } from "expo-router";

import { IssueDetailView } from "@/components/issue/issue-detail-view";

// Same screen body as inbox/issue/[id] — Stack route per tab keeps each
// tab's nav stack independent (matches Linear / Twitter behavior).
export default function MyIssuesIssueDetailScreen() {
  const { id } = useLocalSearchParams<{ id: string }>();
  return id ? <IssueDetailView issueId={id} /> : null;
}
