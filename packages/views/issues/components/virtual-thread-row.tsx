"use client";

import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { keepThreadRowAnchored } from "../utils/keep-thread-rows-anchored";

/** Keep anchors addressable without parsing every offscreen Markdown reply. */
export function VirtualThreadRow({ id, taskId, scrollParent, children }: {
  id: string;
  taskId?: string;
  scrollParent?: HTMLElement | null;
  children: () => ReactNode;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const height = useRef(320);
  const [visible, setVisible] = useState(false);
  const mounted = !scrollParent || visible;

  useEffect(() => {
    const node = ref.current;
    if (!node || !scrollParent) return;
    if (typeof IntersectionObserver !== "function") {
      setVisible(true);
      return;
    }
    const observer = new IntersectionObserver((entries) => {
      const intersecting = entries.at(-1)?.isIntersecting ?? false;
      // Editing and open controls own transient state; don't discard it on scroll.
      const engaged = node.contains(document.activeElement)
        || !!node.querySelector('[contenteditable="true"], [aria-expanded="true"]');
      setVisible(intersecting || engaged);
    }, { root: scrollParent, rootMargin: "800px 0px" });
    observer.observe(node);
    return () => observer.disconnect();
  }, [scrollParent]);

  // The placeholder's height becomes the real one in this commit. Keep the
  // content below a row above the viewport from moving (see the helper).
  useLayoutEffect(() => {
    const node = ref.current;
    if (!node || !visible || !scrollParent) return;
    keepThreadRowAnchored(scrollParent, node, height.current);
  }, [visible, scrollParent]);

  useEffect(() => {
    const node = ref.current;
    if (!node || !mounted) return;
    const measure = () => {
      const next = node.getBoundingClientRect().height;
      if (next > 0) height.current = next;
    };
    measure();
    if (typeof ResizeObserver !== "function") return;
    const observer = new ResizeObserver(measure);
    observer.observe(node);
    return () => observer.disconnect();
  }, [mounted]);

  return <div ref={ref} id={id} data-run-slot-id={taskId}
    data-virtual-thread-row data-virtual-thread-mounted={mounted}
    style={mounted ? undefined : { height: height.current }}>
    {mounted ? children() : null}
  </div>;
}
