"use client";

import { useEffect, useState } from "react";
import type { SearchDocResult } from "@multica/core/types";
import { searchDocs } from "@multica/core/search-index";

/** Doc hits from the server, with the query they answer. */
export interface DocSearchResults {
  query: string;
  docs: SearchDocResult[];
}

const NO_DOCS: DocSearchResults = { query: "", docs: [] };
/**
 * Doc search always goes to the server (the local index does not hold docs),
 * so it keeps its own debounce even when issue search answers locally on every
 * keystroke, and its own request so it never holds back those rows.
 */
export const DOC_SEARCH_DEBOUNCE_MS = 300;

/** Server doc search for the palette. An empty or disabled query clears it. */
export function useDocSearch(query: string, enabled: boolean): DocSearchResults {
  const [results, setResults] = useState<DocSearchResults>(NO_DOCS);
  const q = enabled ? query.trim() : "";

  useEffect(() => {
    if (!q) {
      setResults(NO_DOCS);
      return;
    }
    const controller = new AbortController();
    const timer = setTimeout(() => {
      searchDocs({ q, limit: 10, signal: controller.signal }).then(
        (res) => {
          if (!controller.signal.aborted) setResults({ query: q, docs: res.docs });
        },
        () => {
          if (!controller.signal.aborted) setResults({ query: q, docs: [] });
        },
      );
    }, DOC_SEARCH_DEBOUNCE_MS);
    return () => {
      clearTimeout(timer);
      controller.abort();
    };
  }, [q]);

  return results;
}
