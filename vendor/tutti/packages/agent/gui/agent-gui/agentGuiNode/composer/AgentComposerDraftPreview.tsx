import { useMemo } from "react";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { useState, type CSSProperties } from "react";
import { Spinner } from "@tutti-os/ui-system";
import { ImageIcon, X } from "lucide-react";
import { ZoomableImage } from "../../../app/renderer/components/ZoomableImage";
import { cn } from "../../../app/renderer/lib/utils";
import type { AgentComposerDraftImage } from "../model/agentGuiNodeTypes";
import {
  DRAFT_IMAGE_PREVIEW_BASE_HEIGHT_PX,
  DRAFT_IMAGE_PREVIEW_MAX_RATIO,
  DRAFT_IMAGE_PREVIEW_MAX_WIDTH_PX,
  DRAFT_IMAGE_PREVIEW_MIN_RATIO,
  DRAFT_IMAGE_PREVIEW_MIN_WIDTH_PX
} from "./AgentComposerChrome";
import { observer } from "mobx-react-lite";
export const AgentComposerDraftImagePreview = observer(
  AgentComposerDraftImagePreviewRender
);
export const SendFilledIcon = observer(SendFilledIconRender);

function AgentComposerDraftImagePreviewRender({
  image,
  removeLabel,
  onRemove
}: {
  image: AgentComposerDraftImage;
  removeLabel: string;
  onRemove: (id: string) => void;
}): React.JSX.Element {
  const [aspectRatio, setAspectRatio] = useState(1);
  const previewWidth = Math.round(
    Math.min(
      DRAFT_IMAGE_PREVIEW_MAX_WIDTH_PX,
      Math.max(
        DRAFT_IMAGE_PREVIEW_MIN_WIDTH_PX,
        aspectRatio * DRAFT_IMAGE_PREVIEW_BASE_HEIGHT_PX
      )
    )
  );
  const previewStyle = useMemo(
    () =>
      ({
        aspectRatio: String(aspectRatio),
        width: `${previewWidth}px`
      }) satisfies CSSProperties,
    [aspectRatio, previewWidth]
  );

  const onClickProp1 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onClick"]>
  >(() => onRemove(image.id));
  return (
    <div
      className={cn(
        "group relative min-w-0 overflow-hidden rounded-[6px] border border-[var(--line-1)] bg-[var(--background-fronted)]",
        "[&>[data-rmiz]]:block [&>[data-rmiz]]:size-full",
        "[&>[data-rmiz]>[data-rmiz-content]]:block [&>[data-rmiz]>[data-rmiz-content]]:size-full",
        image.uploadError &&
          "border-[color:color-mix(in_srgb,var(--danger)_55%,var(--line-1))]"
      )}
      data-testid="agent-gui-composer-image-draft"
      data-uploading={image.uploading ? "true" : undefined}
      data-upload-error={image.uploadError ? "true" : undefined}
      style={previewStyle}
    >
      {image.previewUrl ? (
        <ZoomableImageBinding1 image={image} setAspectRatio={setAspectRatio} />
      ) : (
        <div
          className="grid size-full place-items-center text-[var(--text-tertiary)]"
          data-testid="agent-gui-composer-image-preview-pending"
        >
          <ImageIcon aria-hidden size={22} strokeWidth={1.8} />
        </div>
      )}
      {image.uploading ? (
        <div
          className="absolute inset-0 grid place-items-center bg-[color-mix(in_srgb,var(--background-fronted)_62%,transparent)]"
          data-testid="agent-gui-composer-image-uploading"
        >
          <Spinner
            className="text-[var(--text-primary)]"
            size={18}
            strokeWidth={2.4}
            trackColor="var(--transparency-hover)"
            testId="agent-gui-composer-image-upload-spinner"
          />
        </div>
      ) : null}
      <button
        type="button"
        className="absolute right-1 top-1 z-[2] inline-flex size-5 items-center justify-center rounded-full border border-[color-mix(in_srgb,var(--text-primary)_16%,transparent)] bg-[color-mix(in_srgb,var(--background-fronted)_88%,transparent)] text-[var(--text-primary)] opacity-90 shadow-sm transition hover:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[color:color-mix(in_srgb,var(--text-primary)_34%,transparent)]"
        aria-label={removeLabel}
        title={removeLabel}
        onClick={onClickProp1}
      >
        <X size={12} strokeWidth={2.4} aria-hidden />
      </button>
    </div>
  );
}

function SendFilledIconRender(): React.JSX.Element {
  "use memo";
  return (
    <svg
      width="24"
      height="24"
      viewBox="0 0 24 24"
      fill="none"
      aria-hidden="true"
    >
      <path
        d="M2.74311 8.80587C2.84592 8.40096 3.14571 8.08844 3.54551 7.97033L18.5197 3.51569C18.9336 3.39383 19.3809 3.5054 19.6881 3.81262C19.9951 4.11984 20.1076 4.56798 19.9857 4.9817L15.5311 19.9559C15.413 20.3557 15.1005 20.6555 14.6956 20.7583C14.2895 20.8597 13.869 20.7438 13.5721 20.4469L10.455 15.1823C10.8585 14.6483 12.1563 12.9094 14.3475 9.96528C14.6086 9.70419 14.6382 9.31168 14.4138 9.08692C14.1891 8.86221 13.796 8.8913 13.5348 9.15252L8.31088 13.0423L3.05316 9.92799C2.7562 9.63104 2.64049 9.21071 2.74311 8.80587Z"
        fill="currentColor"
      />
    </svg>
  );
}

const ZoomableImageBinding1 = observer(function ZoomableImageBinding1({
  image,
  setAspectRatio
}: ZoomableImageBinding1Props) {
  const onLoadProp2 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof ZoomableImage>["onLoad"]>
  >((event) => {
    const element = event.currentTarget;
    const width = element.naturalWidth;
    const height = element.naturalHeight;
    if (width <= 0 || height <= 0) {
      return;
    }
    const nextRatio = Math.min(
      DRAFT_IMAGE_PREVIEW_MAX_RATIO,
      Math.max(DRAFT_IMAGE_PREVIEW_MIN_RATIO, width / height)
    );
    setAspectRatio(nextRatio);
  });
  return (
    <ZoomableImage
      src={image.previewUrl}
      alt={image.name}
      className="size-full object-contain"
      draggable={false}
      downloadName={image.name || "image.png"}
      onLoad={onLoadProp2}
    />
  );
});

type ZoomableImageBinding1Props = {
  image: NonNullable<{
    image: AgentComposerDraftImage;
    removeLabel: string;
    onRemove: (id: string) => void;
  }>["image"];
  setAspectRatio: import("react").Dispatch<
    import("react").SetStateAction<number>
  >;
};
