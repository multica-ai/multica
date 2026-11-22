"use client";

import { useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useRequiredWorkspaceSlug } from "@multica/core/paths";
import { linkListOptions, workspaceLandingPath } from "@multica/core/links";
import { Link } from "@multica/ui/icons";
import { CollectionPageState } from "../layout/collection-page";
import { useNavigation } from "../navigation";
import { useT } from "../i18n";

export function WorkspaceEntry({ onResolve }: { onResolve?: (path: string) => void }) {
  const wsId = useWorkspaceId();
  const slug = useRequiredWorkspaceSlug();
  const navigation = useNavigation();
  const { t } = useT("settings");
  const { data, isError, isPending, isFetching } = useQuery({
    ...linkListOptions(wsId),
    staleTime: 0,
  });
  const target = isPending || isFetching ? null : workspaceLandingPath(slug, isError ? [] : data ?? []);
  const replace = onResolve ?? navigation.replace;
  useEffect(() => {
    if (target) replace(target);
  }, [target, replace]);
  return <CollectionPageState icon={Link} title={t(($) => $.links.loading)} />;
}
