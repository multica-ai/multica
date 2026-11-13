"use client";

import { useEffect, useRef, useState } from "react";
import type { SearchIssueResult, SearchProjectResult } from "@multica/core/types";
import { isLocalSearchReady, searchIssues, searchProjects } from "@multica/core/search-index";

export interface SearchResults {
  /**
   * The trimmed query these rows answer. Rows outlive the query that fetched
   * them — they stay painted while the next request is in flight — so every
   * result set carries the question it is the answer to, and anything that can
   * act on a row checks that it still matches what the user typed.
   */
  query: string;
  issues: SearchIssueResult[];
  projects: SearchProjectResult[];
}

export const NO_RESULTS: SearchResults = { query: "", issues: [], projects: [] };

/**
 * Issue (and optionally project) search for the palette. An empty query
 * clears the results without a request.
 */
export function useServerSearch(query: string, options: { projects: boolean }) {
  const [results, setResults] = useState<SearchResults>(NO_RESULTS);
  const [isLoading, setIsLoading] = useState(false);
  const withProjects = options.projects;
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const abortRef = useRef<AbortController | null>(null);
  const q = query.trim();

  useEffect(() => {
    if (debounceRef.current) clearTimeout(debounceRef.current);
    if (abortRef.current) abortRef.current.abort();

    if (!q) {
      setResults(NO_RESULTS);
      setIsLoading(false);
      return;
    }

    setIsLoading(true);
    // The debounce spares the server a request per keystroke; the local index
    // answers in milliseconds, so it searches on every keystroke.
    const delay = isLocalSearchReady() ? 0 : 300;
    debounceRef.current = setTimeout(async () => {
      const controller = new AbortController();
      abortRef.current = controller;
      try {
        const [issueRes, projectRes] = await Promise.all([
          searchIssues({ q, limit: 20, include_closed: true, signal: controller.signal }),
          withProjects
            ? searchProjects({ q, limit: 10, include_closed: true, signal: controller.signal })
            : Promise.resolve({ projects: [] as SearchProjectResult[] }),
        ]);
        if (!controller.signal.aborted) {
          setResults({ query: q, issues: issueRes.issues, projects: projectRes.projects });
          setIsLoading(false);
        }
      } catch {
        if (!controller.signal.aborted) {
          // Drop the previous query's rows rather than leaving them on screen
          // permanently greyed out: the request that would have replaced them
          // is never coming. The list falls through to the empty state.
          setResults({ query: q, issues: [], projects: [] });
          setIsLoading(false);
        }
      }
    }, delay);
  }, [q, withProjects]);

  useEffect(
    () => () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
      if (abortRef.current) abortRef.current.abort();
    },
    [],
  );

  // Rows answering an earlier query are still painted while the next request
  // is in flight — that is what keeps the list from strobing on every
  // keystroke — but they must not be actionable: cmdk re-selects the first
  // valid item whenever the input changes, so a live stale row turns the next
  // Enter into a jump to a result the user has already typed past.
  const stale = results.query !== q;
  return { results, isLoading, stale };
}
