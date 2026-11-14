import { useEffect, useMemo, useState } from "react";
import { useNavigation } from "@react-navigation/native";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { issueRowModel } from "../../model/issues";
import { useActorResolver } from "../../model/people";
import { useStatusPresentation } from "../../model/status";
import { useWorkspace } from "../../model/workspace-context";
import { SearchView } from "./SearchView";

const DEBOUNCE_MS = 250;

export function SearchScreen() {
  const navigation = useNavigation();
  const { workspace } = useWorkspace();
  const resolveActor = useActorResolver();
  const status = useStatusPresentation(workspace.id);
  const [query, setQuery] = useState("");
  const [term, setTerm] = useState("");

  useEffect(() => {
    const timer = setTimeout(() => setTerm(query.trim()), DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [query]);

  const search = useQuery({
    queryKey: ["mobile-search", workspace.id, term],
    queryFn: ({ signal }) => api.searchIssues({ q: term, limit: 30, include_closed: true, signal }),
    enabled: term.length > 0,
    placeholderData: keepPreviousData,
    staleTime: 30_000,
  });

  const results = useMemo(
    () => (term ? (search.data?.issues ?? []).map((issue) => issueRowModel(issue, status, resolveActor)) : []),
    [term, search.data, status, resolveActor],
  );

  return (
    <SearchView
      query={query}
      onQueryChange={setQuery}
      state={!term ? "idle" : search.isError ? "error" : search.isFetching && !search.data ? "loading" : search.isFetching ? "loading" : "ready"}
      results={results}
      onRetry={() => void search.refetch()}
      onOpen={(issue) => navigation.navigate("Issue", { issueId: issue.id })}
      onClose={() => navigation.goBack()}
    />
  );
}
