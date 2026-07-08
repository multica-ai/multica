/**
 * Hook that returns a FlatList ref and scrolls the list to offset 0 whenever
 * the watched value changes.
 *
 * Used by every search-enabled picker body to reset scroll when the filter
 * query changes. UISearchController does NOT do this for us — system iOS
 * apps work because they swap in a separate `searchResultsController`; the
 * RN pattern reuses the same FlatList for browse and filter, so without
 * this reset the filtered list sits below the previously-scrolled viewport
 * and looks blank.
 *
 * `animated: false` is deliberate: `animated: true` produces competing
 * scroll tweens during fast typing (RN does not cancel in-flight scroll
 * animations before starting a new one).
 */
import { useEffect, useRef } from "react";
import type { FlatList } from "react-native";

export function useScrollToTopOnChange<T>(value: T) {
  const ref = useRef<FlatList<any>>(null);

  useEffect(() => {
    ref.current?.scrollToOffset({ offset: 0, animated: false });
  }, [value]);

  return ref;
}
