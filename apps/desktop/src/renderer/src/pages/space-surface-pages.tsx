import { useParams } from "react-router-dom";
import {
  SpaceIssuesPage,
  SpaceProjectsPage,
  SpaceAutopilotsPage,
  SpaceDetailPage,
} from "@multica/views/spaces";

// Router wrappers: resolve the :spaceKey param and hand it to the shared
// space surface pages (packages/views owns the actual rendering).

export function SpaceIssuesRoute() {
  const { spaceKey } = useParams<{ spaceKey: string }>();
  if (!spaceKey) return null;
  return <SpaceIssuesPage spaceKey={spaceKey} />;
}

export function SpaceProjectsRoute() {
  const { spaceKey } = useParams<{ spaceKey: string }>();
  if (!spaceKey) return null;
  return <SpaceProjectsPage spaceKey={spaceKey} />;
}

export function SpaceAutopilotsRoute() {
  const { spaceKey } = useParams<{ spaceKey: string }>();
  if (!spaceKey) return null;
  return <SpaceAutopilotsPage spaceKey={spaceKey} />;
}

export function SpaceDetailRoute() {
  const { spaceKey } = useParams<{ spaceKey: string }>();
  if (!spaceKey) return null;
  return <SpaceDetailPage spaceKey={spaceKey} />;
}
