import Image from "next/image";

type ScreenshotProps = {
  src: string;
  alt: string;
  width: number;
  height: number;
  caption?: string;
  priority?: boolean;
};

export function Screenshot({
  src,
  alt,
  width,
  height,
  caption,
  priority = false,
}: ScreenshotProps) {
  return (
    <figure className="my-10 -mx-4 sm:mx-0">
      <div className="overflow-hidden border border-[#0a0d12]/8 bg-[#f5f5f5]">
        <Image
          src={src}
          alt={alt}
          width={width}
          height={height}
          sizes="(max-width: 720px) 100vw, 720px"
          quality={85}
          priority={priority}
          className="block h-auto w-full"
        />
      </div>
      {caption ? (
        <figcaption className="mt-3 text-center text-[13px] text-[#0a0d12]/45">
          {caption}
        </figcaption>
      ) : null}
    </figure>
  );
}
