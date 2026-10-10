/**
 * Minimal iOS-style stack navigator in pure React Native (Animated + native
 * driver). Replaces expo-router/react-navigation, whose native screens and
 * gesture-handler ports are not validated on the RNOH 0.82 matrix
 * (apps/mobile-harmony/AGENTS.md). Screens below the top stay mounted, so
 * scroll positions and query caches survive push/pop exactly like the iOS
 * native stack.
 *
 * The route type is owned by the app shell (app-shell.tsx); this module is
 * route-agnostic and just animates an array of route states.
 */
import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  Animated,
  BackHandler,
  Dimensions,
  Easing,
  StyleSheet,
  View,
  type StyleProp,
  type ViewStyle,
} from "react-native";

const PUSH_ANIM_MS = 280;
const POP_ANIM_MS = 240;

export type StackNav<R> = {
  push: (route: R) => void;
  pop: () => void;
  /** Replace the whole stack — used by workspace switches and auth resets. */
  reset: (route: R) => void;
  /** Pop every route above the first occurrence of `name` (no-op if absent). */
  popTo: (name: string) => void;
  canPop: () => boolean;
};

type StackEntry<R> = {
  key: number;
  route: R;
  anim: Animated.Value;
  /** Set while the pop animation plays; the entry unmounts when it ends. */
  exiting: boolean;
};

const NavContext = createContext<StackNav<never> | null>(null);

export function useNav<R>(): StackNav<R> {
  const nav = useContext(NavContext);
  if (!nav) {
    throw new Error("useNav must be used inside <StackNavigator>");
  }
  return nav as unknown as StackNav<R>;
}

export function StackNavigator<R extends { name: string }>({
  initialRoute,
  renderRoute,
  navRef,
  style,
}: {
  initialRoute: R;
  renderRoute: (route: R) => React.ReactNode;
  /**
   * Receives the nav object during render, so a component ABOVE the
   * navigator (the app shell: auth bootstrap, 401 handling) can drive
   * navigation from effects. Event-time calls inside renderRoute read the
   * same ref after it has been assigned.
   */
  navRef?: { current: StackNav<R> | null };
  style?: StyleProp<ViewStyle>;
}) {
  const keyRef = useRef(1);
  // Entries[0] is the stack base: always mounted, never animated.
  const [entries, setEntries] = useState<StackEntry<R>[]>(() => [
    { key: 0, route: initialRoute, anim: new Animated.Value(1), exiting: false },
  ]);
  const entriesRef = useRef(entries);
  entriesRef.current = entries;

  const pop = useCallback(() => {
    const current = entriesRef.current;
    if (current.length <= 1) return;
    const top = current[current.length - 1];
    if (top.exiting) return;
    setEntries([...current.slice(0, -1), { ...top, exiting: true }]);
    Animated.timing(top.anim, {
      toValue: 0,
      duration: POP_ANIM_MS,
      easing: Easing.in(Easing.cubic),
      useNativeDriver: true,
    }).start(() => {
      setEntries((latest) => latest.filter((e) => e.key !== top.key));
    });
  }, []);

  const push = useCallback((route: R) => {
    setEntries((current) => {
      const top = current[current.length - 1];
      if (top.exiting) return current;
      const anim = new Animated.Value(0);
      const key = keyRef.current++;
      Animated.timing(anim, {
        toValue: 1,
        duration: PUSH_ANIM_MS,
        easing: Easing.out(Easing.cubic),
        useNativeDriver: true,
      }).start();
      return [...current, { key, route, anim, exiting: false }];
    });
  }, []);

  const reset = useCallback((route: R) => {
    setEntries((current) => {
      // Animate every overlay out, then drop to the single new base.
      for (const entry of current.slice(1)) {
        Animated.timing(entry.anim, {
          toValue: 0,
          duration: POP_ANIM_MS,
          easing: Easing.in(Easing.cubic),
          useNativeDriver: true,
        }).start();
      }
      const survivors = [current[0]];
      setTimeout(() => {
        setEntries([
          {
            key: keyRef.current++,
            route,
            anim: new Animated.Value(1),
            exiting: false,
          },
        ]);
      }, POP_ANIM_MS);
      return survivors;
    });
  }, []);

  const popTo = useCallback((name: string) => {
    const current = entriesRef.current;
    const index = current.findIndex((e) => e.route.name === name && !e.exiting);
    if (index < 0 || index === current.length - 1) return;
    const removedKeys = new Set(current.slice(index + 1).map((e) => e.key));
    setEntries(
      current
        .slice(0, index + 1)
        .concat(current.slice(index + 1).map((e) => ({ ...e, exiting: true }))),
    );
    for (const entry of current.slice(index + 1)) {
      Animated.timing(entry.anim, {
        toValue: 0,
        duration: POP_ANIM_MS,
        easing: Easing.in(Easing.cubic),
        useNativeDriver: true,
      }).start(() => {
        setEntries((latest) => latest.filter((e) => !removedKeys.has(e.key)));
      });
    }
  }, []);

  const nav = useMemo<StackNav<R>>(
    () => ({
      push,
      pop,
      reset,
      popTo,
      canPop: () => entriesRef.current.length > 1,
    }),
    [push, pop, reset, popTo],
  );
  if (navRef) navRef.current = nav;

  // Hardware back (HarmonyOS navigation) pops the stack. Screens that need
  // custom back behavior register their own BackHandler after this one —
  // RN semantics give the most-recent listener precedence.
  useEffect(() => {
    const sub = BackHandler.addEventListener("hardwareBackPress", () => {
      const handled = entriesRef.current.length > 1;
      if (handled) pop();
      return handled;
    });
    return () => sub.remove();
  }, [pop]);

  const screenWidth = Dimensions.get("window").width;
  return (
    <NavContext.Provider value={nav as unknown as StackNav<never>}>
      <View style={[styles.fill, style]}>
        {entries.map((entry, index) => {
          const isBase = index === 0;
          const isTop = index === entries.length - 1;
          // Percent strings crash RNOH's native animated C++ node
          // (ValueAnimatedNode::getValueAsDouble assert) — use pixels.
          const translateX = entry.anim.interpolate({
            inputRange: [0, 1],
            outputRange: [screenWidth, 0],
          });
          // Screens covered by the top screen slide 30% left, mirroring the
          // iOS push parallax. Exiting entries keep their own animation.
          const underTranslate = entry.anim.interpolate({
            inputRange: [0, 1],
            outputRange: [0, -screenWidth * 0.3],
          });
          return (
            <Animated.View
              key={entry.key}
              pointerEvents={isTop ? "auto" : "none"}
              style={[
                styles.card,
                !isBase && styles.overlay,
                entry.exiting || isTop
                  ? { transform: [{ translateX }] }
                  : { transform: [{ translateX: underTranslate }] },
              ]}
            >
              {renderRoute(entry.route)}
            </Animated.View>
          );
        })}
      </View>
    </NavContext.Provider>
  );
}

const styles = StyleSheet.create({
  fill: { flex: 1, overflow: "hidden" },
  card: { flex: 1, backgroundColor: "transparent" },
  overlay: {
    ...StyleSheet.absoluteFillObject,
    elevation: 8,
    backgroundColor: "#000",
  },
});
