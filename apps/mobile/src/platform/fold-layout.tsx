import type { ComponentType } from "react";
import { Platform, View, type NativeSyntheticEvent, type ViewProps } from "react-native";
import { requireNativeView } from "expo";

/** Where a fold divides a view, in the view's coordinates. */
export interface Fold {
  x: number;
  y: number;
  width: number;
  height: number;
  /** Room around the fold, included in the frame, that interactive content should keep clear of. */
  margins: { top: number; left: number; bottom: number; right: number };
  /** Folded far enough to divide the screen; a flat-open fold is inactive. */
  active: boolean;
}

interface NativeFold {
  x: number;
  y: number;
  width: number;
  height: number;
  marginTop: number;
  marginLeft: number;
  marginBottom: number;
  marginRight: number;
  active: number;
}

export interface Insets {
  top: number;
  left: number;
  bottom: number;
  right: number;
}

/** What the view reports: its folds, and its safe area as UIKit sees it, system tab bar included. */
export interface FoldLayout {
  folds: Fold[];
  safeArea: Insets;
}

type NativeProps = ViewProps & { onFoldChange?: (event: NativeSyntheticEvent<{ folds: NativeFold[]; safeArea: Insets }>) => void };

function nativeView(): ComponentType<NativeProps> | null {
  if (Platform.OS !== "ios") return null;
  try {
    return requireNativeView<NativeProps>("FoldLayout");
  } catch {
    // A build without the module; it reports no folds.
    return null;
  }
}

const NativeFoldLayout = nativeView();

/** A view that reports its layout, via `modules/fold-layout`. Without the module it reports nothing. */
export function FoldLayoutView({ onLayoutChange, ...props }: ViewProps & { onLayoutChange: (layout: FoldLayout) => void }) {
  if (!NativeFoldLayout) return <View {...props} />;
  return (
    <NativeFoldLayout
      {...props}
      onFoldChange={(event) =>
        onLayoutChange({
          folds: event.nativeEvent.folds.map((fold) => ({
            x: fold.x,
            y: fold.y,
            width: fold.width,
            height: fold.height,
            margins: { top: fold.marginTop, left: fold.marginLeft, bottom: fold.marginBottom, right: fold.marginRight },
            active: fold.active === 1,
          })),
          safeArea: event.nativeEvent.safeArea,
        })
      }
    />
  );
}
