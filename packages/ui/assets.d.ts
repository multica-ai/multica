// Static asset imports inside this package (brand-avatar-assets/*.webp).
// Mirrors packages/views/assets.d.ts: electron-vite / vite resolve them to a
// URL string, Next.js (apps/web) to a StaticImageData object — declare the
// union so both consumers typecheck.
interface StaticImageAsset {
  src: string;
  height?: number;
  width?: number;
  blurDataURL?: string;
}

declare module "*.webp" {
  const src: string | StaticImageAsset;
  export default src;
}
