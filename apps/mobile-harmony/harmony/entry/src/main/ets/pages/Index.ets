import {
  AnyJSBundleProvider,
  MetroJSBundleProvider,
  RNApp,
  RNOHErrorDialog,
  ResourceJSBundleProvider,
  RNOHCoreContext
} from '@rnoh/react-native-openharmony';
import { getRNOHPackages } from '../PackageProvider';

@Entry
@Component
struct Index {
  @StorageLink('RNOHCoreContext') private rnohCoreContext: RNOHCoreContext | undefined = undefined

  build() {
    Column() {
      if (this.rnohCoreContext) {
        if (this.rnohCoreContext?.isDebugModeEnabled) {
          RNOHErrorDialog({ ctx: this.rnohCoreContext })
        }
        RNApp({
          rnInstanceConfig: {
            name: "MulticaHarmony",
            createRNPackages: getRNOHPackages,
            // Ionicons glyphs (components/ui/icon.tsx) render as font text
            // against this bundled TTF — the react-native-vector-icons
            // approach, no native icon component needed.
            fontResourceByFontFamily: {
              "Ionicons": $rawfile('Ionicons.ttf'),
            },
            enableDebugger: this.rnohCoreContext?.isDebugModeEnabled,
          },
          appKey: "MulticaHarmony",
          // Plain JS instead of Hermes bytecode: HBC bundles compiled by
          // RN 0.77's hermesc execute but AppRegistry registration never
          // lands in the RNOH 0.77 runtime ("has not been registered" on
          // launch, white screen). Revisit HBC once the platform accepts it.
          jsBundleProvider: this.rnohCoreContext?.isDebugModeEnabled ?
            new AnyJSBundleProvider([
              new MetroJSBundleProvider(),
              new ResourceJSBundleProvider(this.rnohCoreContext.uiAbilityContext.resourceManager, 'bundle.harmony.js'),
            ]) :
            new ResourceJSBundleProvider(this.rnohCoreContext.uiAbilityContext.resourceManager, 'bundle.harmony.js'),
        })
      }
    }
    .height('100%')
    .width('100%')
  }
}
