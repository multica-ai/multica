/**
 * Multica's own RNOH package: registers the app's ArkTS TurboModules via the
 * RNOH 0.82 contract (getAnyThreadTurboModuleFactoryByNameMap). No custom
 * components, so the wrapped-builder map from RNOHPackage stays empty.
 *
 * Also hosts the RNCNetInfo registration (vendored minimal implementation
 * in MulticaNetInfoModule.ts — the netinfo tpl package cannot be imported
 * on this matrix; CPP proxy: cpp/PackageProvider.cpp).
 */
import {
  RNOHPackage,
  AnyThreadTurboModule,
  AnyThreadTurboModuleContext,
  UITurboModule,
  UITurboModuleContext,
} from '@rnoh/react-native-openharmony';
import { MulticaSecureStorageModule } from './MulticaSecureStorageModule';
import { MulticaClipboardModule } from './MulticaClipboardModule';
import { MulticaHapticsModule } from './MulticaHapticsModule';
import { MulticaMediaPickerModule } from './MulticaMediaPickerModule';
import { MulticaSafeAreaModule } from './MulticaSafeAreaModule';
import { MulticaNetInfoModule } from './MulticaNetInfoModule';

type AnyThreadTurboModuleByNameFactory = (ctx: AnyThreadTurboModuleContext) => AnyThreadTurboModule | null
type UITurboModuleByNameFactory = (ctx: UITurboModuleContext) => UITurboModule | null

export class MulticaPackage extends RNOHPackage {
  getAnyThreadTurboModuleFactoryByNameMap(): Map<string, AnyThreadTurboModuleByNameFactory> {
    const map = new Map<string, AnyThreadTurboModuleByNameFactory>()
    map.set('MulticaSecureStorage', (ctx: AnyThreadTurboModuleContext) => new MulticaSecureStorageModule(ctx))
    map.set('MulticaClipboard', (ctx: AnyThreadTurboModuleContext) => new MulticaClipboardModule(ctx))
    map.set('MulticaHaptics', (ctx: AnyThreadTurboModuleContext) => new MulticaHapticsModule(ctx))
    map.set('MulticaMediaPicker', (ctx: AnyThreadTurboModuleContext) => new MulticaMediaPickerModule(ctx))
    map.set('MulticaSafeArea', (ctx: AnyThreadTurboModuleContext) => new MulticaSafeAreaModule(ctx))
    return map
  }

  getUITurboModuleFactoryByNameMap(): Map<string, UITurboModuleByNameFactory> {
    const map = new Map<string, UITurboModuleByNameFactory>()
    map.set('RNCNetInfo', (ctx: UITurboModuleContext) => new MulticaNetInfoModule(ctx))
    return map
  }
}
