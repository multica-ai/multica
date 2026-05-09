import { useMemo, useRef, useState, type ReactNode } from "react";
import { Pressable, StyleSheet, View } from "react-native";
import type { GestureResponderEvent, LayoutChangeEvent } from "react-native";
import { X } from "lucide-react-native";
import { useTranslation } from "react-i18next";
import { colors, spacing } from "../../theme/tokens";

export type FloatingActionMenuAction = {
  key: string;
  label: string;
  icon: ReactNode;
  onPress: () => void;
};

const FAB_SIZE = 56;
const FAB_ACTION_SIZE = 46;
const FAB_EDGE = spacing.xl;
const FAB_RADIUS = 76;
const FAB_HIT_RADIUS = 46;
const FAB_MIN_SWIPE_DISTANCE = 34;
const FAB_MAX_SWIPE_DISTANCE = 126;
const FAB_ANGLE_HIT_DEGREES = 28;
const FAB_ACTION_ANGLES = [115, 160];

export function FloatingActionMenu({
  actions,
  mainIcon,
}: {
  actions: FloatingActionMenuAction[];
  mainIcon: ReactNode;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [activeKey, setActiveKey] = useState<string | null>(null);
  const [containerSize, setContainerSize] = useState({ width: 0, height: 0 });
  const containerRef = useRef<View>(null);
  const containerOriginRef = useRef({ x: 0, y: 0 });
  const draggingRef = useRef(false);
  const startedOpenRef = useRef(false);

  const mainCenter = useMemo(() => ({
    x: containerSize.width - FAB_EDGE - FAB_SIZE / 2,
    y: containerSize.height - FAB_EDGE - FAB_SIZE / 2,
  }), [containerSize.height, containerSize.width]);

  const actionPositions = useMemo(
    () =>
      actions.map((action, index) => {
        const angle = (FAB_ACTION_ANGLES[index] ?? 180) * Math.PI / 180;
        return {
          action,
          angleDegrees: FAB_ACTION_ANGLES[index] ?? 180,
          centerX: mainCenter.x + Math.cos(angle) * FAB_RADIUS,
          centerY: mainCenter.y - Math.sin(angle) * FAB_RADIUS,
        };
      }),
    [actions, mainCenter.x, mainCenter.y],
  );

  function measureContainer() {
    containerRef.current?.measureInWindow((x, y) => {
      containerOriginRef.current = { x, y };
    });
  }

  function handleLayout(event: LayoutChangeEvent) {
    setContainerSize(event.nativeEvent.layout);
    measureContainer();
  }

  function close() {
    setOpen(false);
    setActiveKey(null);
    draggingRef.current = false;
  }

  function selectAction(action: FloatingActionMenuAction) {
    close();
    action.onPress();
  }

  function getActiveKey(event: GestureResponderEvent) {
    const origin = containerOriginRef.current;
    const x = event.nativeEvent.pageX - origin.x;
    const y = event.nativeEvent.pageY - origin.y;
    let nextKey: string | null = null;
    let closestDistance = Number.POSITIVE_INFINITY;

    for (const item of actionPositions) {
      const dx = x - item.centerX;
      const dy = y - item.centerY;
      const distance = Math.hypot(dx, dy);
      if (distance < closestDistance && distance <= FAB_HIT_RADIUS) {
        closestDistance = distance;
        nextKey = item.action.key;
      }
    }

    if (nextKey) return nextKey;

    const dx = x - mainCenter.x;
    const dy = mainCenter.y - y;
    const distanceFromMain = Math.hypot(dx, dy);
    if (
      distanceFromMain < FAB_MIN_SWIPE_DISTANCE ||
      distanceFromMain > FAB_MAX_SWIPE_DISTANCE
    ) {
      return null;
    }

    const pointerAngle = normalizeDegrees(Math.atan2(dy, dx) * 180 / Math.PI);
    let nearestActionKey: string | null = null;
    let nearestAngleDistance = Number.POSITIVE_INFINITY;
    for (const item of actionPositions) {
      const angleDistance = circularDistance(pointerAngle, item.angleDegrees);
      if (angleDistance < nearestAngleDistance) {
        nearestAngleDistance = angleDistance;
        nearestActionKey = item.action.key;
      }
    }

    return nearestAngleDistance <= FAB_ANGLE_HIT_DEGREES ? nearestActionKey : null;
  }

  function updateActive(event: GestureResponderEvent) {
    setActiveKey(getActiveKey(event));
  }

  function handleRelease(event: GestureResponderEvent) {
    const releasedKey = getActiveKey(event) ?? activeKey;
    const selected = actions.find((action) => action.key === releasedKey);
    if (draggingRef.current && selected) {
      selectAction(selected);
      return;
    }
    draggingRef.current = false;
  }

  return (
    <View
      ref={containerRef}
      pointerEvents="box-none"
      style={styles.layer}
      onLayout={handleLayout}
    >
      {open ? (
        <Pressable
          accessibilityLabel={t("common.close_quick_actions")}
          accessibilityRole="button"
          onPress={close}
          style={styles.backdrop}
        />
      ) : null}
      {open ? (
        <View pointerEvents="box-none" style={styles.actionsLayer}>
          {actionPositions.map(({ action, centerX, centerY }) => {
            const active = activeKey === action.key;
            return (
              <View
                key={action.key}
                style={[
                  styles.actionWrap,
                  {
                    left: centerX - FAB_ACTION_SIZE / 2,
                    top: centerY - FAB_ACTION_SIZE / 2,
                  },
                ]}
              >
                <Pressable
                  accessibilityLabel={action.label}
                  accessibilityRole="button"
                  onPress={() => selectAction(action)}
                  style={({ pressed }) => [
                    styles.actionButton,
                    active && styles.actionButtonActive,
                    pressed && styles.buttonPressed,
                  ]}
                >
                  {action.icon}
                </Pressable>
              </View>
            );
          })}
        </View>
      ) : null}
      <View
        accessibilityLabel={open ? t("common.close_quick_actions") : t("common.open_quick_actions")}
        accessibilityRole="button"
        onResponderGrant={() => {
          measureContainer();
          startedOpenRef.current = open;
          draggingRef.current = false;
          setActiveKey(null);
          if (!open) setOpen(true);
        }}
        onResponderMove={(event) => {
          draggingRef.current = true;
          updateActive(event);
        }}
        onResponderRelease={(event: GestureResponderEvent) => {
          if (draggingRef.current) {
            handleRelease(event);
            return;
          }
          if (startedOpenRef.current) close();
        }}
        onResponderTerminate={() => {
          draggingRef.current = false;
          setActiveKey(null);
        }}
        onStartShouldSetResponder={() => true}
        style={styles.mainButton}
      >
        {open ? (
          <X color={colors.primaryForeground} size={24} strokeWidth={2.3} />
        ) : mainIcon}
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  layer: {
    bottom: 0,
    left: 0,
    position: "absolute",
    right: 0,
    top: 0,
    zIndex: 60,
  },
  backdrop: {
    backgroundColor: "transparent",
    bottom: 0,
    left: 0,
    position: "absolute",
    right: 0,
    top: 0,
  },
  actionsLayer: {
    bottom: 0,
    left: 0,
    position: "absolute",
    right: 0,
    top: 0,
  },
  actionWrap: {
    position: "absolute",
  },
  actionButton: {
    alignItems: "center",
    backgroundColor: colors.primary,
    borderRadius: FAB_ACTION_SIZE / 2,
    elevation: 9,
    height: FAB_ACTION_SIZE,
    justifyContent: "center",
    shadowColor: "#000000",
    shadowOffset: { height: 4, width: 0 },
    shadowOpacity: 0.18,
    shadowRadius: 9,
    width: FAB_ACTION_SIZE,
  },
  actionButtonActive: {
    transform: [{ scale: 1.08 }],
  },
  mainButton: {
    alignItems: "center",
    backgroundColor: colors.primary,
    borderRadius: FAB_SIZE / 2,
    bottom: FAB_EDGE,
    elevation: 8,
    height: FAB_SIZE,
    justifyContent: "center",
    position: "absolute",
    right: FAB_EDGE,
    shadowColor: "#000000",
    shadowOffset: { height: 4, width: 0 },
    shadowOpacity: 0.18,
    shadowRadius: 10,
    width: FAB_SIZE,
  },
  buttonPressed: {
    opacity: 0.82,
  },
});

function normalizeDegrees(value: number) {
  return ((value % 360) + 360) % 360;
}

function circularDistance(a: number, b: number) {
  const diff = Math.abs(normalizeDegrees(a) - normalizeDegrees(b));
  return Math.min(diff, 360 - diff);
}
