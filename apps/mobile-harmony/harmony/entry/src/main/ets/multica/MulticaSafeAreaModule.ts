/**
 * Safe-area insets from the system avoid areas, in physical pixels (the JS
 * side divides by PixelRatio). The vendored tpl safe-area module resolves
 * all-zero insets in common window states (constructor busy-waits for the
 * window; getSafeAreaInsets returns zeros when the window is not
 * layout-full-screen), which collapsed the tab bar's bottom padding onto the
 * gesture indicator. This module is the authoritative source on this matrix.
 */
import { AnyThreadTurboModule } from '@rnoh/react-native-openharmony/ts';
import type { AnyThreadTurboModuleContext } from '@rnoh/react-native-openharmony/ts';
import window from '@ohos.window';

export class MulticaSafeAreaModule extends AnyThreadTurboModule {
  constructor(protected ctx: AnyThreadTurboModuleContext) {
    super(ctx);
  }

  async getInsets(): Promise<object> {
    try {
      const win = await window.getLastWindow(this.ctx.uiAbilityContext);
      const sys = win.getWindowAvoidArea(window.AvoidAreaType.TYPE_SYSTEM);
      const nav = win.getWindowAvoidArea(window.AvoidAreaType.TYPE_NAVIGATION_INDICATOR);
      return {
        top: sys.topRect.height,
        bottom: nav.bottomRect.height,
        left: sys.leftRect.width,
        right: sys.rightRect.width,
      };
    } catch (e) {
      console.error(`[MulticaSafeArea] getInsets failed: ${JSON.stringify(e)}`);
      return { top: 0, bottom: 0, left: 0, right: 0 };
    }
  }
}
