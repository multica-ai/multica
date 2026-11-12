"use client";

import { useEffect, useState } from "react";
import { pinBodyVisible, sameIds } from "./comment-pins";

export function useVisiblePinBodies(container: HTMLElement | null, topInset = 0) {
  const [visible, setVisible] = useState<ReadonlySet<string>>(() => new Set());
  useEffect(() => {
    if (!container) return;
    const nodes = new Map<Element, string>();
    let frame = 0;
    const update = () => {
      frame = 0;
      const viewport = container.getBoundingClientRect();
      const bottomInset = stickyBottomInset(container, viewport);
      const next = new Set<string>();
      for (const [node, id] of nodes) {
        if (node.isConnected && pinBodyVisible(node.getBoundingClientRect(), viewport, bottomInset, topInset)) next.add(id);
      }
      setVisible(old => sameIds(old, next) ? old : next);
    };
    const schedule = () => { if (!frame) frame = requestAnimationFrame(update); };
    // Scroll and mutation listeners still drive updates where
    // IntersectionObserver is unavailable (older embedded webviews, jsdom).
    const observer = typeof IntersectionObserver === "undefined" ? null
      : new IntersectionObserver(schedule, { root: container, rootMargin: `-${topInset}px 0px 0px 0px`, threshold: [0, 0.01, 0.1, 1] });
    const resize = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(schedule);
    const overlays = new Set<Element>();
    const bind = () => {
      const mounted = new Map<Element, string>();
      container.querySelectorAll<HTMLElement>("[data-comment-pin-body]").forEach(node => {
        const id = node.dataset.commentPinBody;
        if (id) mounted.set(node, id);
      });
      for (const node of nodes.keys()) if (!mounted.has(node)) observer?.unobserve(node);
      for (const node of mounted.keys()) if (!nodes.has(node)) observer?.observe(node);
      nodes.clear();
      mounted.forEach((id, node) => nodes.set(node, id));
      const liveOverlays = new Set(container.querySelectorAll("[data-comment-pins-bottom-overlay]"));
      for (const node of overlays) if (!liveOverlays.has(node)) { resize?.unobserve(node); overlays.delete(node); }
      for (const node of liveOverlays) if (!overlays.has(node)) { resize?.observe(node); overlays.add(node); }
      schedule();
    };
    const mutations = new MutationObserver(records => {
      if (records.some(record => !(record.target instanceof Element && record.target.closest("[data-comment-pins-overlay]")))) bind();
    });
    mutations.observe(container, { subtree: true, childList: true, attributes: true, attributeFilter: ["data-comment-pin-body"] });
    container.addEventListener("scroll", schedule, { passive: true });
    window.addEventListener("resize", schedule);
    bind();
    return () => {
      observer?.disconnect(); mutations.disconnect(); resize?.disconnect(); cancelAnimationFrame(frame);
      container.removeEventListener("scroll", schedule); window.removeEventListener("resize", schedule);
    };
  }, [container, topInset]);
  return visible;
}

// Sticky bottom overlays (the comment composer) mark themselves with
// data-comment-pins-bottom-overlay; only the part actually covering the
// viewport's bottom edge counts.
function stickyBottomInset(container: HTMLElement, viewport: DOMRect) {
  let inset = 0;
  container.querySelectorAll<HTMLElement>("[data-comment-pins-bottom-overlay]").forEach(node => {
    if (getComputedStyle(node).position !== "sticky") return;
    const rect = node.getBoundingClientRect();
    if (rect.bottom >= viewport.bottom - 1 && rect.top < viewport.bottom) inset = Math.max(inset, viewport.bottom - rect.top);
  });
  return inset;
}
