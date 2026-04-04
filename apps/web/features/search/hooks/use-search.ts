import { useState, useEffect, useRef, useCallback } from "react";
import { api } from "@/shared/api";
import type { SearchIssueResult } from "@/shared/types/api";

interface UseSearchResult {
  results: SearchIssueResult[];
  isLoading: boolean;
  search: (query: string) => void;
  clear: () => void;
}

export function useSearch(debounceMs = 300): UseSearchResult {
  const [results, setResults] = useState<SearchIssueResult[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const abortRef = useRef<AbortController | null>(null);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const queryRef = useRef("");

  const clear = useCallback(() => {
    queryRef.current = "";
    setResults([]);
    setIsLoading(false);
    if (timerRef.current) clearTimeout(timerRef.current);
    if (abortRef.current) abortRef.current.abort();
  }, []);

  const search = useCallback(
    (query: string) => {
      if (timerRef.current) clearTimeout(timerRef.current);
      if (abortRef.current) abortRef.current.abort();

      const trimmed = query.trim();
      queryRef.current = trimmed;

      if (!trimmed) {
        setResults([]);
        setIsLoading(false);
        return;
      }

      // Don't set isLoading here — avoid replacing the result list with a
      // spinner on every keystroke.  Only flip to loading once the debounce
      // fires and an actual API call starts.

      timerRef.current = setTimeout(async () => {
        const controller = new AbortController();
        abortRef.current = controller;

        setIsLoading(true);

        try {
          const res = await api.search({ q: trimmed, limit: 10 });
          if (!controller.signal.aborted) {
            setResults(res.issues);
            setIsLoading(false);
          }
        } catch {
          if (!controller.signal.aborted) {
            setResults([]);
            setIsLoading(false);
          }
        }
      }, debounceMs);
    },
    [debounceMs],
  );

  useEffect(() => {
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current);
      if (abortRef.current) abortRef.current.abort();
    };
  }, []);

  return { results, isLoading, search, clear };
}
