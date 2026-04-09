import { useState, useEffect, useRef, useCallback } from "react";
import { useLocation } from "react-router-dom";

export function useNavigationHistory() {
  const [canGoBack, setCanGoBack] = useState(false);
  const [canGoForward, setCanGoForward] = useState(false);
  const location = useLocation();

  const seqRef = useRef(1);
  const maxSeqRef = useRef(1);
  const isNavRef = useRef(false);

  // Seed initial entry + listen for popstate (browser back/forward)
  useEffect(() => {
    window.history.replaceState({ seq: 1 }, "");

    const handlePopState = (event: PopStateEvent) => {
      const seq = (event.state?.seq as number) ?? 0;
      seqRef.current = seq;
      setCanGoBack(seq > 1);
      setCanGoForward(seq < maxSeqRef.current);
    };

    window.addEventListener("popstate", handlePopState);
    return () => window.removeEventListener("popstate", handlePopState);
  }, []);

  // Stamp seq on each new navigation
  useEffect(() => {
    // Skip if this was our own goBack/goForward call
    if (isNavRef.current) {
      isNavRef.current = false;
      return;
    }

    const nextSeq = seqRef.current + 1;
    seqRef.current = nextSeq;
    maxSeqRef.current = nextSeq;
    window.history.replaceState({ seq: nextSeq }, "");
    setCanGoBack(nextSeq > 1);
    setCanGoForward(false);
  }, [location]);

  const goBack = useCallback(() => {
    if (!canGoBack) return;
    isNavRef.current = true;
    window.history.back();
  }, [canGoBack]);

  const goForward = useCallback(() => {
    if (!canGoForward) return;
    isNavRef.current = true;
    window.history.forward();
  }, [canGoForward]);

  return { canGoBack, canGoForward, goBack, goForward };
}
