import { Slot } from "radix-ui";
import { useMemo } from "react";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { observer } from "mobx-react-lite";
import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
  type FocusEvent,
  type JSX,
  type MouseEvent,
  type WheelEvent
} from "react";
import type { AgentConversationFollowEndMode } from "../agentConversationFollowEndController";
import type { AgentMessageLocatorItem } from "./agentTranscriptModel";
import {
  findMessageLocatorScrollParent,
  useAgentMessageLocatorSelection,
  type AgentMessageLocatorVirtualSelectionSource
} from "./useAgentMessageLocatorSelection";

export { findMessageLocatorScrollParent } from "./useAgentMessageLocatorSelection";

const AGENT_MESSAGE_LOCATOR_PANEL_FADE_MS = 160;
const AGENT_MESSAGE_LOCATOR_ITEM_SPACING_PX = 30;
const AGENT_MESSAGE_LOCATOR_HIT_SIZE_PX = 36;
const AGENT_MESSAGE_LOCATOR_BOTTOM_SAFE_INSET_MAX_PX = 48;
const AGENT_MESSAGE_LOCATOR_SCROLL_DURATION_MS = 160;
const agentMessageLocatorScrollAnimations = new WeakMap<HTMLElement, number>();

interface AgentMessageLocatorVisibleFrame {
  heightPx: number;
  topOffsetPx: number;
}

export const AgentMessageLocatorRail = observer(
  function AgentMessageLocatorRail({
    followEndMode,
    items,
    isVisible = true,
    label,
    onLocate,
    virtualSelectionSource
  }: AgentMessageLocatorRailProps): JSX.Element | null {
    const handleLocateItemCallback = usePersistCallback<
      typeof handleLocateItem
    >((...args) => handleLocateItem(...args));

    const handleViewportClickCallback = usePersistCallback<
      typeof handleViewportClick
    >((...args) => handleViewportClick(...args));

    const handleViewportMouseMoveCallback = usePersistCallback<
      typeof handleViewportMouseMove
    >((...args) => handleViewportMouseMove(...args));

    const openPanelCallback = usePersistCallback<typeof openPanel>((...args) =>
      openPanel(...args)
    );

    const closePanelSoonCallback = usePersistCallback<typeof closePanelSoon>(
      (...args) => closePanelSoon(...args)
    );

    const handleBlurCaptureCallback = usePersistCallback<
      typeof handleBlurCapture
    >((...args) => handleBlurCapture(...args));

    const locatorRef = useRef<HTMLElement | null>(null);
    const locatorViewportRef = useRef<HTMLDivElement | null>(null);
    const closePanelTimeoutRef = useRef<number | null>(null);
    const [isPanelOpen, setIsPanelOpen] = useState(false);
    const [shouldRenderPanel, setShouldRenderPanel] = useState(false);
    const [activeKey, setActiveKey] = useState<string | null>(null);
    const { selectItem, selectedKey } = useAgentMessageLocatorSelection({
      followEndMode,
      items,
      isVisible,
      locatorRef,
      virtualSelectionSource
    });
    const previousAgentResponseByKeyRef = useRef<ReadonlyMap<
      string,
      boolean
    > | null>(null);
    const [unreadAgentResponseKeys, setUnreadAgentResponseKeys] = useState<
      ReadonlySet<string>
    >(new Set());
    const [visibleFrame, setVisibleFrame] =
      useState<AgentMessageLocatorVisibleFrame | null>(null);
    useEffect(() => {
      if (isPanelOpen) {
        setShouldRenderPanel(true);
        return;
      }
      // timing: keep the panel mounted through its close-fade transition
      const timeout = window.setTimeout(
        () => setShouldRenderPanel(false),
        AGENT_MESSAGE_LOCATOR_PANEL_FADE_MS
      );
      return () => window.clearTimeout(timeout);
    }, [isPanelOpen]);
    useEffect(
      () => () => {
        if (closePanelTimeoutRef.current !== null) {
          window.clearTimeout(closePanelTimeoutRef.current);
        }
      },
      []
    );
    useEffect(() => {
      const previousAgentResponseByKey = previousAgentResponseByKeyRef.current;
      const currentKeys = new Set(items.map((item) => item.key));

      setUnreadAgentResponseKeys((currentUnreadKeys) => {
        let nextUnreadKeys: Set<string> | null = null;
        const ensureNextUnreadKeys = (): Set<string> => {
          if (!nextUnreadKeys) {
            nextUnreadKeys = new Set(currentUnreadKeys);
          }
          return nextUnreadKeys;
        };

        for (const key of currentUnreadKeys) {
          if (!currentKeys.has(key)) {
            ensureNextUnreadKeys().delete(key);
          }
        }

        if (previousAgentResponseByKey) {
          for (const item of items) {
            const hadAgentResponse =
              previousAgentResponseByKey.get(item.key) ?? false;
            if (
              previousAgentResponseByKey.has(item.key) &&
              item.hasAgentResponse &&
              !hadAgentResponse &&
              item.key !== selectedKey
            ) {
              ensureNextUnreadKeys().add(item.key);
            }
          }
        }

        return nextUnreadKeys ?? currentUnreadKeys;
      });

      previousAgentResponseByKeyRef.current = new Map(
        items.map((item) => [item.key, item.hasAgentResponse])
      );
    }, [items, selectedKey]);
    useEffect(() => {
      if (!selectedKey) {
        return;
      }
      setUnreadAgentResponseKeys((currentUnreadKeys) => {
        if (!currentUnreadKeys.has(selectedKey)) {
          return currentUnreadKeys;
        }
        const nextUnreadKeys = new Set(currentUnreadKeys);
        nextUnreadKeys.delete(selectedKey);
        return nextUnreadKeys;
      });
    }, [selectedKey]);
    useLayoutEffect(() => {
      if (!isVisible) {
        return;
      }
      const locator = locatorRef.current;
      const scrollParent = locator
        ? findMessageLocatorScrollParent(locator)
        : null;
      if (!scrollParent) {
        return;
      }

      let animationFrame: number | null = null;
      const updateVisibleFrame = (): void => {
        animationFrame = null;
        const nextFrame = readMessageLocatorVisibleFrame(scrollParent);
        setVisibleFrame((current) =>
          current?.heightPx === nextFrame.heightPx &&
          current.topOffsetPx === nextFrame.topOffsetPx
            ? current
            : nextFrame
        );
      };
      const scheduleUpdate = (): void => {
        if (animationFrame !== null) {
          return;
        }
        animationFrame = window.requestAnimationFrame(updateVisibleFrame);
      };

      scheduleUpdate();
      const resizeObserver =
        typeof ResizeObserver === "undefined"
          ? null
          : new ResizeObserver(scheduleUpdate);
      resizeObserver?.observe(scrollParent);
      window.addEventListener("resize", scheduleUpdate);
      return () => {
        resizeObserver?.disconnect();
        window.removeEventListener("resize", scheduleUpdate);
        if (animationFrame !== null) {
          window.cancelAnimationFrame(animationFrame);
        }
      };
    }, [isVisible, items.length]);
    const selectedIndex = selectedKey
      ? items.findIndex((item) => item.key === selectedKey)
      : -1;
    const itemCount = items.length;
    const visibleHeight = visibleFrame?.heightPx ?? null;
    // Reading scrollTop forces layout, so this runs only when the answer can change:
    // a new list of the same items arrives with every committed update.
    useLayoutEffect(() => {
      if (!isVisible) {
        return;
      }
      const viewport = locatorViewportRef.current;
      if (selectedIndex < 0 || !viewport) {
        return;
      }

      const railHeight =
        (itemCount - 1) * AGENT_MESSAGE_LOCATOR_ITEM_SPACING_PX +
        AGENT_MESSAGE_LOCATOR_HIT_SIZE_PX;
      const viewportHeight = Math.min(railHeight, visibleHeight ?? railHeight);
      scrollMessageLocatorViewportToIndex(
        viewport,
        selectedIndex,
        viewportHeight
      );
    }, [isVisible, itemCount, selectedIndex, visibleHeight]);

    if (items.length < 2) {
      return null;
    }

    const railHeight =
      (items.length - 1) * AGENT_MESSAGE_LOCATOR_ITEM_SPACING_PX +
      AGENT_MESSAGE_LOCATOR_HIT_SIZE_PX;
    const viewportHeight =
      visibleFrame === null
        ? railHeight
        : Math.min(railHeight, visibleFrame.heightPx);
    const activeOrSelectedKey = activeKey ?? selectedKey;
    const markItemRead = (itemKey: string): void => {
      setUnreadAgentResponseKeys((currentUnreadKeys) => {
        if (!currentUnreadKeys.has(itemKey)) {
          return currentUnreadKeys;
        }
        const nextUnreadKeys = new Set(currentUnreadKeys);
        nextUnreadKeys.delete(itemKey);
        return nextUnreadKeys;
      });
    };
    const handleLocateItem = (item: AgentMessageLocatorItem): void => {
      selectItem(item.key);
      setActiveKey(item.key);
      markItemRead(item.key);
      onLocate(item);
    };

    const itemFromPointerEvent = (
      event: MouseEvent<HTMLElement>
    ): AgentMessageLocatorItem | null => {
      const viewport = locatorViewportRef.current;
      if (!viewport) {
        return null;
      }
      const rect = viewport.getBoundingClientRect();
      const pointerY = event.clientY - rect.top + viewport.scrollTop;
      const index = Math.round(
        (pointerY - AGENT_MESSAGE_LOCATOR_HIT_SIZE_PX / 2) /
          AGENT_MESSAGE_LOCATOR_ITEM_SPACING_PX
      );
      return items[Math.max(0, Math.min(items.length - 1, index))] ?? null;
    };
    const handleViewportClick = (event: MouseEvent<HTMLDivElement>): void => {
      if ((event.target as HTMLElement | null)?.closest("button")) {
        return;
      }
      const item = itemFromPointerEvent(event);
      if (item) {
        handleLocateItem(item);
      }
    };

    const handleViewportMouseMove = (
      event: MouseEvent<HTMLDivElement>
    ): void => {
      const item = itemFromPointerEvent(event);
      if (item) {
        setActiveKey(item.key);
      }
    };

    const openPanel = (): void => {
      if (closePanelTimeoutRef.current !== null) {
        window.clearTimeout(closePanelTimeoutRef.current);
        closePanelTimeoutRef.current = null;
      }
      setIsPanelOpen(true);
    };

    const closePanelSoon = (): void => {
      if (closePanelTimeoutRef.current !== null) {
        window.clearTimeout(closePanelTimeoutRef.current);
      }
      // timing: delay closing so pointer can move from trigger into panel content
      closePanelTimeoutRef.current = window.setTimeout(() => {
        closePanelTimeoutRef.current = null;
        setIsPanelOpen(false);
        setActiveKey(null);
      }, 120);
    };

    const closePanelNow = (): void => {
      if (closePanelTimeoutRef.current !== null) {
        window.clearTimeout(closePanelTimeoutRef.current);
        closePanelTimeoutRef.current = null;
      }
      setIsPanelOpen(false);
      setActiveKey(null);
    };
    const handleBlurCapture = (event: FocusEvent<HTMLElement>): void => {
      const nextTarget = event.relatedTarget;
      if (
        nextTarget instanceof Node &&
        event.currentTarget.contains(nextTarget)
      ) {
        return;
      }
      closePanelNow();
    };

    return (
      <NavBinding1
        locatorRef={locatorRef}
        label={label}
        items={items}
        handleBlurCaptureCallback={handleBlurCaptureCallback}
        openPanelCallback={openPanelCallback}
        closePanelSoonCallback={closePanelSoonCallback}
        railHeight={railHeight}
        viewportHeight={viewportHeight}
        visibleFrame={visibleFrame}
        locatorViewportRef={locatorViewportRef}
        handleViewportClickCallback={handleViewportClickCallback}
        handleViewportMouseMoveCallback={handleViewportMouseMoveCallback}
        selectedKey={selectedKey}
        unreadAgentResponseKeys={unreadAgentResponseKeys}
        handleLocateItemCallback={handleLocateItemCallback}
        setActiveKey={setActiveKey}
        shouldRenderPanel={shouldRenderPanel}
        isPanelOpen={isPanelOpen}
        activeOrSelectedKey={activeOrSelectedKey}
      />
    );
  }
);

function scrollMessageLocatorViewportToIndex(
  viewport: HTMLElement,
  selectedIndex: number,
  viewportHeight: number
): void {
  const selectedTop =
    selectedIndex * AGENT_MESSAGE_LOCATOR_ITEM_SPACING_PX -
    AGENT_MESSAGE_LOCATOR_HIT_SIZE_PX / 2;
  const selectedBottom = selectedTop + AGENT_MESSAGE_LOCATOR_HIT_SIZE_PX;
  const padding = AGENT_MESSAGE_LOCATOR_HIT_SIZE_PX;
  const currentTop = viewport.scrollTop;
  const currentBottom = currentTop + viewportHeight;

  if (selectedTop < currentTop + padding) {
    viewport.scrollTop = Math.max(0, selectedTop - padding);
    return;
  }

  if (selectedBottom > currentBottom - padding) {
    viewport.scrollTop = Math.max(0, selectedBottom - viewportHeight + padding);
  }
}

export function scrollTranscriptRowIntoView(
  row: HTMLElement,
  scrollParent: HTMLElement | null
): void {
  if (!scrollParent) {
    row.scrollIntoView({
      block: "center",
      behavior: "smooth"
    });
    return;
  }

  const targetScrollTop = targetScrollTopForTranscriptRow(row, scrollParent);
  animateMessageLocatorScroll(scrollParent, targetScrollTop);
}

function targetScrollTopForTranscriptRow(
  row: HTMLElement,
  scrollParent: HTMLElement
): number {
  const rowRect = row.getBoundingClientRect();
  const scrollParentRect = scrollParent.getBoundingClientRect();
  const visibleFrame = readMessageLocatorVisibleFrame(scrollParent);
  const rowCenter =
    rowRect.top -
    scrollParentRect.top +
    scrollParent.scrollTop +
    rowRect.height / 2;
  const maxScrollTop = Math.max(
    0,
    scrollParent.scrollHeight - scrollParent.clientHeight
  );
  const targetScrollTop =
    rowCenter - visibleFrame.topOffsetPx - visibleFrame.heightPx / 2;
  return Math.min(maxScrollTop, Math.max(0, targetScrollTop));
}

function animateMessageLocatorScroll(
  scrollParent: HTMLElement,
  targetScrollTop: number
): void {
  const previousAnimation =
    agentMessageLocatorScrollAnimations.get(scrollParent);
  if (previousAnimation !== undefined) {
    window.cancelAnimationFrame(previousAnimation);
  }

  const startScrollTop = scrollParent.scrollTop;
  const delta = targetScrollTop - startScrollTop;
  const startedAt = performance.now();

  const step = (now: number): void => {
    const progress = Math.min(
      1,
      (now - startedAt) / AGENT_MESSAGE_LOCATOR_SCROLL_DURATION_MS
    );
    scrollParent.scrollTop = startScrollTop + delta * progress;
    if (progress >= 1) {
      scrollParent.scrollTop = targetScrollTop;
      agentMessageLocatorScrollAnimations.delete(scrollParent);
      return;
    }
    agentMessageLocatorScrollAnimations.set(
      scrollParent,
      window.requestAnimationFrame(step)
    );
  };

  agentMessageLocatorScrollAnimations.set(
    scrollParent,
    window.requestAnimationFrame(step)
  );
}

function readMessageLocatorVisibleFrame(
  scrollParent: HTMLElement
): AgentMessageLocatorVisibleFrame {
  const style = window.getComputedStyle(scrollParent);
  const topOffsetPx = parseCssPx(style.scrollPaddingTop);
  const bottomOffsetPx = Math.min(
    parseCssPx(style.scrollPaddingBottom),
    AGENT_MESSAGE_LOCATOR_BOTTOM_SAFE_INSET_MAX_PX
  );
  return {
    heightPx: Math.max(
      0,
      scrollParent.clientHeight - topOffsetPx - bottomOffsetPx
    ),
    topOffsetPx
  };
}

function parseCssPx(value: string): number {
  const parsed = Number.parseFloat(value);
  return Number.isFinite(parsed) ? Math.max(0, parsed) : 0;
}

function containMessageLocatorPanelWheel(
  event: WheelEvent<HTMLDivElement>
): void {
  event.stopPropagation();
  if (event.deltaY === 0) {
    return;
  }
  event.preventDefault();
  event.currentTarget.scrollTop += event.deltaY;
}

const ButtonBinding1 = observer(function ButtonBinding1({
  item,
  index,
  selectedKey,
  unreadAgentResponseKeys,
  handleLocateItem,
  setActiveKey,
  ...slotProps
}: ButtonBinding1Props &
  Omit<React.ComponentProps<typeof Slot.Root>, keyof ButtonBinding1Props>) {
  const onClickProp1 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onClick"]>
  >(() => handleLocateItem(item));
  const onFocusProp2 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onFocus"]>
  >(() => setActiveKey(item.key));
  const onMouseEnterProp3 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onMouseEnter"]>
  >(() => setActiveKey(item.key));
  const styleProp1Value1 = `${
    index * AGENT_MESSAGE_LOCATOR_ITEM_SPACING_PX +
    AGENT_MESSAGE_LOCATOR_HIT_SIZE_PX / 2
  }px`;
  const styleProp1 = useMemo<
    NonNullable<React.ComponentProps<"button">["style"]>
  >(
    () =>
      ({
        "--agent-message-locator-position": styleProp1Value1
      }) as CSSProperties & Record<`--${string}`, string>,
    [styleProp1Value1]
  );
  return (
    <Slot.Root {...slotProps}>
      <button
        key={item.key}
        type="button"
        className="agent-gui-message-locator__tick nodrag tsh-desktop-no-drag"
        style={styleProp1}
        aria-label={item.summary}
        title={item.summary}
        data-selected={item.key === selectedKey ? "true" : undefined}
        data-agent-message-locator-turn-group-index={item.turnGroupIndex}
        data-multica-locator-turn-id={(item as { turnId?: string }).turnId}
        data-unread-agent-response={
          unreadAgentResponseKeys.has(item.key) ? "true" : undefined
        }
        onClick={onClickProp1}
        onFocus={onFocusProp2}
        onMouseEnter={onMouseEnterProp3}
      >
        <span className="agent-gui-message-locator__dot" aria-hidden="true" />
      </button>
    </Slot.Root>
  );
});

const ButtonBinding2 = observer(function ButtonBinding2({
  item,
  activeOrSelectedKey,
  handleLocateItem,
  setActiveKey,
  ...slotProps
}: ButtonBinding2Props &
  Omit<React.ComponentProps<typeof Slot.Root>, keyof ButtonBinding2Props>) {
  const onClickProp4 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onClick"]>
  >(() => handleLocateItem(item));
  const onFocusProp5 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onFocus"]>
  >(() => setActiveKey(item.key));
  const onMouseEnterProp6 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onMouseEnter"]>
  >(() => setActiveKey(item.key));
  return (
    <Slot.Root {...slotProps}>
      <button
        key={`panel:${item.key}`}
        type="button"
        className="agent-gui-message-locator__panel-item nodrag tsh-desktop-no-drag"
        data-active={item.key === activeOrSelectedKey ? "true" : undefined}
        onClick={onClickProp4}
        onFocus={onFocusProp5}
        onMouseEnter={onMouseEnterProp6}
      >
        <span className="agent-gui-message-locator__panel-item-text">
          {item.summary}
        </span>
      </button>
    </Slot.Root>
  );
});

const DivBinding1 = observer(function DivBinding1({
  item,
  index,
  ...slotProps
}: {
  item: AgentMessageLocatorItem;
  index: number;
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    item: AgentMessageLocatorItem;
    index: number;
  }
>) {
  const styleProp2Value1 = `${
    AGENT_MESSAGE_LOCATOR_HIT_SIZE_PX / 2 +
    (index + 0.5) * AGENT_MESSAGE_LOCATOR_ITEM_SPACING_PX
  }px`;
  const styleProp2 = useMemo<NonNullable<React.ComponentProps<"div">["style"]>>(
    () =>
      ({
        "--agent-message-locator-segment-position": styleProp2Value1
      }) as CSSProperties & Record<`--${string}`, string>,
    [styleProp2Value1]
  );
  return (
    <Slot.Root {...slotProps}>
      <div
        key={`segment:${item.key}`}
        className="agent-gui-message-locator__track-segment"
        style={styleProp2}
        aria-hidden="true"
      />
    </Slot.Root>
  );
});

const DivBinding2 = observer(function DivBinding2({
  railHeight,
  items,
  selectedKey,
  unreadAgentResponseKeys,
  handleLocateItemCallback,
  setActiveKey,
  ...slotProps
}: DivBinding2Props &
  Omit<React.ComponentProps<typeof Slot.Root>, keyof DivBinding2Props>) {
  const styleProp3Value1 = `${railHeight}px`;
  const styleProp3 = useMemo<NonNullable<React.ComponentProps<"div">["style"]>>(
    () =>
      ({
        "--agent-message-locator-height": styleProp3Value1
      }) as CSSProperties & Record<`--${string}`, string>,
    [styleProp3Value1]
  );
  return (
    <Slot.Root {...slotProps}>
      <div className="agent-gui-message-locator__content" style={styleProp3}>
        {items.slice(0, -1).map((item, index) => (
          <DivBinding1 key={`segment:${item.key}`} item={item} index={index} />
        ))}
        {items.map((item, index) => (
          <ButtonBinding1
            key={item.key}
            item={item}
            index={index}
            selectedKey={selectedKey}
            unreadAgentResponseKeys={unreadAgentResponseKeys}
            handleLocateItem={handleLocateItemCallback}
            setActiveKey={setActiveKey}
          />
        ))}
      </div>
    </Slot.Root>
  );
});

const NavBinding1 = observer(function NavBinding1({
  locatorRef,
  label,
  items,
  handleBlurCaptureCallback,
  openPanelCallback,
  closePanelSoonCallback,
  railHeight,
  viewportHeight,
  visibleFrame,
  locatorViewportRef,
  handleViewportClickCallback,
  handleViewportMouseMoveCallback,
  selectedKey,
  unreadAgentResponseKeys,
  handleLocateItemCallback,
  setActiveKey,
  shouldRenderPanel,
  isPanelOpen,
  activeOrSelectedKey
}: NavBinding1Props) {
  const styleProp4Value1 = `${railHeight}px`;
  const styleProp4Value2 = `${viewportHeight}px`;
  const hasVisibleFrame = visibleFrame !== null;
  const visibleHeight = visibleFrame?.heightPx;
  const visibleOffset = visibleFrame?.topOffsetPx;
  const styleProp4Value3 = useMemo(
    () =>
      hasVisibleFrame
        ? {
            "--agent-message-locator-visible-height": `${visibleHeight}px`,
            "--agent-message-locator-visible-top-offset": `${visibleOffset}px`
          }
        : {},
    [hasVisibleFrame, visibleHeight, visibleOffset]
  );
  const styleProp4 = useMemo<NonNullable<React.ComponentProps<"nav">["style"]>>(
    () =>
      ({
        "--agent-message-locator-height": styleProp4Value1,
        "--agent-message-locator-viewport-height": styleProp4Value2,
        ...styleProp4Value3
      }) as CSSProperties & Record<`--${string}`, string>,
    [styleProp4Value1, styleProp4Value2, styleProp4Value3]
  );
  return (
    <nav
      ref={locatorRef}
      className="agent-gui-message-locator"
      aria-label={label ?? items[0]?.summary}
      data-agent-transcript-scroll-away-intent
      data-testid="agent-message-locator"
      onBlurCapture={handleBlurCaptureCallback}
      onFocusCapture={openPanelCallback}
      onMouseEnter={openPanelCallback}
      onMouseLeave={closePanelSoonCallback}
      style={styleProp4}
    >
      <div
        ref={locatorViewportRef}
        className="agent-gui-message-locator__viewport"
        data-testid="agent-message-locator-viewport"
        onClick={handleViewportClickCallback}
        onMouseMove={handleViewportMouseMoveCallback}
      >
        <DivBinding2
          railHeight={railHeight}
          items={items}
          selectedKey={selectedKey}
          unreadAgentResponseKeys={unreadAgentResponseKeys}
          handleLocateItemCallback={handleLocateItemCallback}
          setActiveKey={setActiveKey}
        />
      </div>
      {shouldRenderPanel ? (
        <div
          className="agent-gui-message-locator__panel"
          role="tooltip"
          data-open={isPanelOpen ? "true" : undefined}
          data-testid="agent-message-locator-panel"
          onMouseEnter={openPanelCallback}
          onMouseLeave={closePanelSoonCallback}
          onWheel={containMessageLocatorPanelWheel}
        >
          {items.map((item) => (
            <ButtonBinding2
              key={`panel:${item.key}`}
              item={item}
              activeOrSelectedKey={activeOrSelectedKey}
              handleLocateItem={handleLocateItemCallback}
              setActiveKey={setActiveKey}
            />
          ))}
        </div>
      ) : null}
    </nav>
  );
});

type AgentMessageLocatorRailProps = {
  followEndMode?: AgentConversationFollowEndMode;
  items: readonly AgentMessageLocatorItem[];
  isVisible?: boolean;
  label?: string;
  onLocate: (item: AgentMessageLocatorItem) => void;
  virtualSelectionSource?: AgentMessageLocatorVirtualSelectionSource;
};

type ButtonBinding1Props = {
  item: AgentMessageLocatorItem;
  index: number;
  selectedKey: string | null;
  unreadAgentResponseKeys: ReadonlySet<string>;
  handleLocateItem: (item: AgentMessageLocatorItem) => void;
  setActiveKey: import("react").Dispatch<
    import("react").SetStateAction<string | null>
  >;
};

type ButtonBinding2Props = {
  item: AgentMessageLocatorItem;
  activeOrSelectedKey: string | null;
  handleLocateItem: (item: AgentMessageLocatorItem) => void;
  setActiveKey: import("react").Dispatch<
    import("react").SetStateAction<string | null>
  >;
};

type DivBinding2Props = {
  railHeight: number;
  items: readonly AgentMessageLocatorItem[];
  selectedKey: string | null;
  unreadAgentResponseKeys: ReadonlySet<string>;
  handleLocateItemCallback: (item: AgentMessageLocatorItem) => void;
  setActiveKey: import("react").Dispatch<
    import("react").SetStateAction<string | null>
  >;
};

type NavBinding1Props = {
  locatorRef: import("react").RefObject<HTMLElement | null>;
  label: string | undefined;
  items: readonly AgentMessageLocatorItem[];
  handleBlurCaptureCallback: (event: FocusEvent<HTMLElement>) => void;
  openPanelCallback: () => void;
  closePanelSoonCallback: () => void;
  railHeight: number;
  viewportHeight: number;
  visibleFrame: AgentMessageLocatorVisibleFrame | null;
  locatorViewportRef: import("react").RefObject<HTMLDivElement | null>;
  handleViewportClickCallback: (event: MouseEvent<HTMLDivElement>) => void;
  handleViewportMouseMoveCallback: (event: MouseEvent<HTMLDivElement>) => void;
  selectedKey: string | null;
  unreadAgentResponseKeys: ReadonlySet<string>;
  handleLocateItemCallback: (item: AgentMessageLocatorItem) => void;
  setActiveKey: import("react").Dispatch<
    import("react").SetStateAction<string | null>
  >;
  shouldRenderPanel: boolean;
  isPanelOpen: boolean;
  activeOrSelectedKey: string | null;
};
