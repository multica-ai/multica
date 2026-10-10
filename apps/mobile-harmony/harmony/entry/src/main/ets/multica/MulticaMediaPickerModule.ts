/**
 * Media/document picking via @ohos.file.picker, mirroring the slice of
 * expo-image-picker / expo-document-picker the app uses (single-select,
 * images-only or any file). The picked URI is copied into the HAP cache
 * directory before resolving:
 *
 *   - Photo-library URIs (`file://media/...`) carry a temporary read
 *     grant that expires — a retry upload (or an upload started after a
 *     Metro reload) could no longer read the source. A sandbox copy is
 *     stable for the lifetime of the cache file.
 *   - The composer feeds the returned URI both to `<Image>` (preview) and
 *     to `api.uploadFile`'s FormData part, so it must be a plain app-
 *     readable `file://` sandbox URI, never a media-library URI.
 *
 * Mirrors the expo behaviour of copying picks into the cache directory
 * (`copyToCacheDirectory: true` on the iOS document picker).
 */
import { AnyThreadTurboModule } from '@rnoh/react-native-openharmony/ts';
import type { AnyThreadTurboModuleContext } from '@rnoh/react-native-openharmony/ts';
import picker from '@ohos.file.picker';
import fs from '@ohos.file.fs';

export class MulticaMediaPickerModule extends AnyThreadTurboModule {
  constructor(protected ctx: AnyThreadTurboModuleContext) {
    super(ctx);
  }

  /**
   * Opens the system photo picker limited to images, single-select.
   * Resolves a `file://` URI in the app cache, or null when the user
   * cancels or the pick/copy fails.
   */
  async pickImage(): Promise<string | null> {
    try {
      const photoPicker = new picker.PhotoViewPicker();
      const options: picker.PhotoSelectOptions = {
        MIMEType: picker.PhotoViewMIMETypes.IMAGE_TYPE,
        maxSelectNumber: 1,
      };
      const result: picker.PhotoSelectResult = await photoPicker.select(options);
      const sourceUri = result.photoUris.length > 0 ? result.photoUris[0] : null;
      if (!sourceUri) {
        // Empty selection = user cancelled the picker sheet.
        return null;
      }
      return await this.copyIntoCache(sourceUri, 'jpg');
    } catch (e) {
      console.error(`[MulticaMediaPicker] pickImage failed: ${JSON.stringify(e)}`);
      return null;
    }
  }

  /**
   * Opens the system document picker (all file types), single-select.
   * Resolves a `file://` URI in the app cache, or null on cancel/failure.
   */
  async pickDocument(): Promise<string | null> {
    try {
      const docPicker = new picker.DocumentViewPicker();
      const options: picker.DocumentSelectOptions = {
        maxSelectNumber: 1,
      };
      const uris = await docPicker.select(options);
      const sourceUri = uris.length > 0 ? uris[0] : null;
      if (!sourceUri) {
        return null;
      }
      return await this.copyIntoCache(sourceUri, '');
    } catch (e) {
      console.error(`[MulticaMediaPicker] pickDocument failed: ${JSON.stringify(e)}`);
      return null;
    }
  }

  /**
   * Copies the picked file into the HAP cache directory and returns the
   * copy's `file://` URI. The destination keeps the source's basename
   * (sanitized) so upload filenames and previews read naturally;
   * collisions are avoided with a time + random prefix.
   */
  private async copyIntoCache(sourceUri: string, fallbackExt: string): Promise<string | null> {
    try {
      const slashAt = sourceUri.lastIndexOf('/');
      let base = slashAt >= 0 ? sourceUri.substring(slashAt + 1) : sourceUri;
      // Strip any query-ish suffix and sanitize to a safe filename.
      const qAt = base.indexOf('?');
      if (qAt >= 0) {
        base = base.substring(0, qAt);
      }
      base = base.replace(/[^A-Za-z0-9._-]/g, '_');
      if (base.length === 0 || base === '.') {
        const ext = fallbackExt.length > 0 ? `.${fallbackExt}` : '';
        base = `pick${ext}`;
      }
      const dotAt = base.lastIndexOf('.');
      const ext = dotAt > 0 ? base.substring(dotAt) : (fallbackExt.length > 0 ? `.${fallbackExt}` : '');
      const stem = dotAt > 0 ? base.substring(0, dotAt) : base;
      const dest = `${this.ctx.uiAbilityContext.cacheDir}/${Date.now().toString(36)}-` +
        `${Math.random().toString(36).slice(2, 8)}-${stem}${ext}`;

      const srcFile = await fs.open(sourceUri, fs.OpenMode.READ_ONLY);
      try {
        const destFile = await fs.open(dest, fs.OpenMode.READ_WRITE | fs.OpenMode.CREATE | fs.OpenMode.TRUNC);
        try {
          await fs.copyFile(srcFile.fd, destFile.fd);
        } finally {
          await fs.close(destFile.fd);
        }
      } finally {
        await fs.close(srcFile.fd);
      }
      return `file://${dest}`;
    } catch (e) {
      console.error(`[MulticaMediaPicker] copyIntoCache failed: ${JSON.stringify(e)}`);
      return null;
    }
  }
}
