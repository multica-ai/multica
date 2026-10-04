/**
 * Safe-area insets for the HarmonyOS client — API-compatible subset of
 * react-native-safe-area-context. The authoritative source is the app-owned
 * MulticaSafeArea TurboModule (system avoid areas in physical px; converted
 * to dp with PixelRatio): the tpl package's module resolves zero insets in
 * common window states, which used to collapse the tab bar's bottom padding
 * onto the gesture indicator. Static fallbacks apply only while no native
 * module is present (old HAP). Insets refresh on foreground so rotation and
 * free-window resizes are picked up.
 *
 * Consumers: `useSafeAreaInsets()` and `<SafeAreaView edges={[...]}/>`.
 */
import React, {
  createContext,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from "react";
import {
  AppState,
  PixelRatio,
  TurboModule,
  TurboModuleRegistry,
  View,
  type AppStateStatus,
  type StyleProp,
  type ViewStyle,
} from "react-native";

type Insets = { top: number; right: number; bottom: number; left: number };

interface MulticaSafeAreaSpec extends TurboModule {
  /** Physical pixels — divide by PixelRatio for dp. */
  getInsets(): Promise<Insets>;
}

const multicaSafeArea =
  TurboModuleRegistry.get<MulticaSafeAreaSpec>("MulticaSafeArea");

const FALLBACK_INSETS: Insets = { top: 44, right: 0, bottom: 24, left: 0 };

const SafeAreaContext = createContext<Insets>(FALLBACK_INSETS);

function resolveInsets(): Promise<Insets> {
  if (!multicaSafeArea) {
    return Promise.resolve(FALLBACK_INSETS);
  }
  return multicaSafeArea
    .getInsets()
    .then((px) => {
      const dpr = PixelRatio.get();
      return {
        top: px.top / dpr,
        bottom: px.bottom / dpr,
        left: px.left / dpr,
        right: px.right / dpr,
      };
    })
    .catch(() => FALLBACK_INSETS);
}

let loggedInsets = false;

export function SafeAreaProvider({ children }: { children: ReactNode }) {
  const [insets, setInsets] = useState<Insets>(FALLBACK_INSETS);

  useEffect(() => {
    let cancelled = false;
    const load = () => {
      void resolveInsets().then((next) => {
        if (cancelled) return;
        setInsets(next);
        if (!loggedInsets) {
          loggedInsets = true;
          console.log(
            `[safe-area] top=${next.top} bottom=${next.bottom} left=${next.left} right=${next.right}`,
          );
        }
      });
    };
    load();
    const sub = AppState.addEventListener("change", (status: AppStateStatus) => {
      if (status === "active") load();
    });
    return () => {
      cancelled = true;
      sub.remove();
    };
  }, []);

  return <SafeAreaContext.Provider value={insets}>{children}</SafeAreaContext.Provider>;
}

export function useSafeAreaInsets(): Insets {
  return useContext(SafeAreaContext);
}

export type SafeAreaEdge = "top" | "bottom" | "left" | "right";

export function SafeAreaView({
  edges = ["top", "bottom", "left", "right"],
  style,
  children,
}: {
  edges?: SafeAreaEdge[];
  style?: StyleProp<ViewStyle>;
  children?: ReactNode;
}) {
  const insets = useSafeAreaInsets();
  const padding: ViewStyle = {
    paddingTop: edges.includes("top") ? insets.top : 0,
    paddingBottom: edges.includes("bottom") ? insets.bottom : 0,
    paddingLeft: edges.includes("left") ? insets.left : 0,
    paddingRight: edges.includes("right") ? insets.right : 0,
  };
  return <View style={[padding, style]}>{children}</View>;
}
