import { resolveWorkspaceImageMimeType } from "@tutti-os/workspace-file-preview";
import { observer } from "mobx-react-lite";
import { useEffect, useState, type JSX } from "react";
import { useOptionalAgentHostApi } from "../../../agentActivityHost";
import { ZoomableImage } from "../../../app/renderer/components/ZoomableImage";
import {
  isLocalImagePath,
  resolveImageGenerationPreviewSrc
} from "../../imageGenerationTool";

interface AgentGeneratedImagePreviewProps {
  uri: string;
  mimeType: string | null;
  alt: string;
  className: string;
}

export const AgentGeneratedImagePreview = observer(
  function AgentGeneratedImagePreview({
    uri,
    mimeType,
    alt,
    className
  }: AgentGeneratedImagePreviewProps): JSX.Element | null {
    "use memo";
    const agentHostApi = useOptionalAgentHostApi();
    const localPath = isLocalImagePath(uri) ? uri.trim() : null;
    const readWorkspaceImage = localPath
      ? agentHostApi?.workspace?.readFile
      : undefined;
    const [src, setSrc] = useState<string | null>(() =>
      !localPath ? resolveImageGenerationPreviewSrc(uri) : null
    );

    useEffect(() => {
      if (!localPath || !readWorkspaceImage) {
        setSrc(resolveImageGenerationPreviewSrc(uri));
        return;
      }

      const resolvedLocalPath = localPath;
      const resolvedReadWorkspaceImage = readWorkspaceImage;
      let canceled = false;
      let objectUrl: string | null = null;
      const resolvedMimeType =
        mimeType?.trim() ||
        resolveWorkspaceImageMimeType(resolvedLocalPath) ||
        "image/png";

      async function loadWorkspaceImage(): Promise<void> {
        try {
          const result = await resolvedReadWorkspaceImage({
            path: resolvedLocalPath
          });
          if (canceled) {
            return;
          }
          const bytes =
            result.bytes instanceof Uint8Array
              ? result.bytes
              : new Uint8Array(result.bytes);
          const arrayBuffer = bytes.buffer.slice(
            bytes.byteOffset,
            bytes.byteOffset + bytes.byteLength
          ) as ArrayBuffer;
          objectUrl = URL.createObjectURL(
            new Blob([arrayBuffer], { type: resolvedMimeType })
          );
          setSrc(objectUrl);
        } catch {
          if (!canceled) {
            setSrc(null);
          }
        }
      }

      setSrc(null);
      void loadWorkspaceImage();

      return () => {
        canceled = true;
        if (objectUrl) {
          URL.revokeObjectURL(objectUrl);
        }
      };
    }, [localPath, mimeType, readWorkspaceImage, uri]);

    if (!src) {
      return null;
    }

    return (
      <ZoomableImage
        alt={alt}
        className={className}
        downloadName={localPath ? localPath.split(/[\\/]/).pop() : "image.png"}
        src={src}
        wrapElement="span"
      />
    );
  }
);
