import { useEffect } from "react";

/** Sets document.title. The tab system observes this automatically. */
export function useDocumentTitle(title: string) {
  useEffect(() => {
    if (title) document.title = title;
  }, [title]);
}
