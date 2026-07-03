import { useParams } from "react-router-dom";
import {
  TeamIssuesPage,
  TeamProjectsPage,
  TeamAutopilotsPage,
  TeamSettingsPage,
} from "@multica/views/teams";

// Router wrappers: resolve the :teamKey param and hand it to the shared
// team surface pages (packages/views owns the actual rendering).

export function TeamIssuesRoute() {
  const { teamKey } = useParams<{ teamKey: string }>();
  if (!teamKey) return null;
  return <TeamIssuesPage teamKey={teamKey} />;
}

export function TeamProjectsRoute() {
  const { teamKey } = useParams<{ teamKey: string }>();
  if (!teamKey) return null;
  return <TeamProjectsPage teamKey={teamKey} />;
}

export function TeamAutopilotsRoute() {
  const { teamKey } = useParams<{ teamKey: string }>();
  if (!teamKey) return null;
  return <TeamAutopilotsPage teamKey={teamKey} />;
}

export function TeamSettingsRoute() {
  const { teamKey } = useParams<{ teamKey: string }>();
  if (!teamKey) return null;
  return <TeamSettingsPage teamKey={teamKey} />;
}
