import { useLocalSearchParams } from "expo-router";

import { IssueDetailView } from "@/components/issue/issue-detail-view";

export default function IssueDetailScreen() {
  const { id } = useLocalSearchParams<{ id: string }>();
  return id ? <IssueDetailView issueId={id} /> : null;
}
