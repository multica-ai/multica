// Asset imports — modern bundlers resolve these to a URL at build time,
// but the exact shape differs between consumers:
//   - electron-vite / plain vite: a `string` URL
//   - Next.js (apps/web): a `StaticImageData` object with `.src`, plus
//     width/height/blurDataURL
// Declare the union here so packages/views compiles in both contexts.
// Component code should normalise with `typeof x === "string" ? x : x.src`.
interface StaticImageAsset {
  src: string;
  height?: number;
  width?: number;
  blurDataURL?: string;
}

declare module "*.png" {
  const src: string | StaticImageAsset;
  export default src;
}
declare module "*.svg" {
  const src: string | StaticImageAsset;
  export default src;
}

declare module "dom-to-image" {
  interface DomToImageOptions {
    width?: number;
    height?: number;
    style?: Record<string, string | number>;
    filter?: (node: HTMLElement) => boolean;
    bgcolor?: string;
    scale?: number;
    quality?: number;
    cacheBust?: boolean;
  }

  function toBlob(node: HTMLElement, options?: DomToImageOptions): Promise<Blob>;
  function toPixelData(node: HTMLElement, options?: DomToImageOptions): Promise<Uint8Array>;

  export default { toBlob, toPixelData };
}
