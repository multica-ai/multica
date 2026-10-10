/**
 * System clipboard access via @ohos.pasteboard, mirroring the slice of
 * expo-clipboard the app uses (plain-text set/read).
 */
import { AnyThreadTurboModule } from '@rnoh/react-native-openharmony/ts';
import type { AnyThreadTurboModuleContext } from '@rnoh/react-native-openharmony/ts';
import pasteboard from '@ohos.pasteboard';

export class MulticaClipboardModule extends AnyThreadTurboModule {
  constructor(protected ctx: AnyThreadTurboModuleContext) {
    super(ctx);
  }

  async setString(text: string): Promise<boolean> {
    try {
      const data = pasteboard.createData(pasteboard.MIMETYPE_TEXT_PLAIN, text);
      await pasteboard.getSystemPasteboard().setData(data);
      return true;
    } catch (e) {
      console.error(`[MulticaClipboard] setString failed: ${JSON.stringify(e)}`);
      return false;
    }
  }

  async getString(): Promise<string> {
    try {
      const data = await pasteboard.getSystemPasteboard().getData();
      return data?.getPrimaryText() ?? '';
    } catch (e) {
      console.error(`[MulticaClipboard] getString failed: ${JSON.stringify(e)}`);
      return '';
    }
  }
}
