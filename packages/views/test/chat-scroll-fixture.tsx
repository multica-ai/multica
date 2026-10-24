import { useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import { flushSync } from "react-dom";
import { Virtuoso, type VirtuosoHandle } from "react-virtuoso";
import { useStickToBottom } from "../chat/components/stick-to-bottom";

let growTail: (px: number) => void;
let appendRow: () => void;
let prependRows: () => void;
let notifyHeight: () => void;
let latePin: () => void;
let stream: ReturnType<typeof setInterval> | undefined;

function Fixture() {
  const [el, setEl] = useState<HTMLDivElement | null>(null);
  const ref = useRef<VirtuosoHandle>(null);
  const [firstItemIndex, setFirstItemIndex] = useState(10000);
  const [rows, setRows] = useState(() => Array.from({ length: 60 }, (_, i) => ({
    id: i, height: 180 + i % 7 * 13,
  })));
  const follow = useStickToBottom(el, ref);
  growTail = (px) => flushSync(() => setRows((old) => old.map((row, i) =>
    i === old.length - 1 ? { ...row, height: row.height + px } : row)));
  appendRow = () => flushSync(() => setRows((old) => [...old, { id: old.length, height: 180 }]));
  prependRows = () => flushSync(() => {
    setFirstItemIndex((old) => old - 10);
    setRows((old) => [...Array.from({ length: 10 }, (_, i) => ({ id: -10 + i, height: 180 })), ...old]);
  });
  notifyHeight = follow.onContentHeightChanged;
  latePin = () => ref.current?.scrollToIndex({ index: "LAST", align: "end" });

  return <div id="chat-scroll" ref={setEl} tabIndex={0}
    data-following={String(follow.isFollowing())}
    data-ready={String(follow.hasReachedLiveEnd)}
    data-follow-disabled={String(follow.followOutput === false)}
    style={{ height: 600, width: 800, overflowY: "auto" }}>
    {el && <Virtuoso ref={ref} customScrollParent={el} data={rows} firstItemIndex={firstItemIndex}
      initialTopMostItemIndex={{ index: "LAST", align: "end" }}
      increaseViewportBy={{ top: 400, bottom: 600 }} atBottomThreshold={120}
      computeItemKey={(_, row) => row.id}
      followOutput={follow.followOutput} totalListHeightChanged={follow.onContentHeightChanged}
      itemContent={(_, row) => <div data-chat-live-end={row.id === rows[rows.length - 1]?.id ? "" : undefined}
        style={{ height: row.height, position: "relative", background: row.id % 2 ? "#eee" : "white" }}>
        <div>Message {row.id}</div>
        <button onKeyDown={(event) => { if (event.key === "Home") event.preventDefault(); }}>Row action</button>
        <input aria-label="Draft" defaultValue="editable text" />
        <pre style={{ height: 60, width: 300, margin: 0, overflowY: "auto", overscrollBehaviorY: "contain" }}>
          {Array.from({ length: 30 }, (_, i) => `code line ${i}`).join("\n")}
        </pre>
        <button data-row-end style={{ position: "absolute", bottom: 0 }}>End of row</button>
      </div>}
    />}
  </div>;
}

export function render() {
  flushSync(() => createRoot(document.getElementById("mount")!).render(<Fixture />));
}
export function grow(px: number) { growTail(px); }
export function append() { appendRow(); }
export function prepend() { prependRows(); }
export function pin() { notifyHeight(); }
export function landOldPin() { latePin(); }
export function startStream() { stream = setInterval(() => growTail(13), 25); }
export function stopStream() { clearInterval(stream); }
