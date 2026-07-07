"use client";

import { useEffect, useRef, useState } from "react";
import { ProgressTracker } from "./progress-tracker";
import type { ProgressStep, ProgressTrackerChoice } from "./progress-tracker";

export type UploadPhase = "presigning" | "uploading" | "completing";

interface UploadShowcaseProps {
  /** The file being uploaded — used for local preview and step description */
  file: File;
  phase: UploadPhase | null;
  /** XHR progress 0–100 */
  progress: number;
  isPending: boolean;
  isSuccess: boolean;
  isError: boolean;
  errorMessage?: string;
}

function deriveSteps(
  phase: UploadPhase | null,
  progress: number,
  isPending: boolean,
  isSuccess: boolean,
  isError: boolean,
  errorMessage: string | undefined,
  fileName: string
): ProgressStep[] {
  const order: UploadPhase[] = ["presigning", "uploading", "completing"];
  const idx = phase ? order.indexOf(phase) : -1;

  function status(i: number): ProgressStep["status"] {
    if (isSuccess) return "completed";
    if (isError) {
      if (i < idx) return "completed";
      if (i === idx) return "failed";
      return "pending";
    }
    if (!isPending) return "pending";
    if (i < idx) return "completed";
    if (i === idx) return "in-progress";
    return "pending";
  }

  return [
    {
      id: "presigning",
      label: "Preparing",
      description: "Generating secure upload URL",
      status: status(0),
    },
    {
      id: "uploading",
      label: "Uploading",
      description:
        phase === "uploading" && progress > 0
          ? `${fileName} — ${Math.round(progress)}%`
          : fileName,
      status: status(1),
    },
    {
      id: "completing",
      label: "Processing",
      description:
        isError && idx === 2 ? (errorMessage ?? "Server error") : "Saving to your workspace",
      status: status(2),
    },
  ];
}

export function UploadShowcase({
  file,
  phase,
  progress,
  isPending,
  isSuccess,
  isError,
  errorMessage,
}: UploadShowcaseProps) {
  const [previewUrl, setPreviewUrl] = useState<string | null>(null);
  const urlRef = useRef<string | null>(null);
  const startRef = useRef<number>(Date.now());
  const [elapsed, setElapsed] = useState(0);

  // Create object URL for local preview
  useEffect(() => {
    const url = URL.createObjectURL(file);
    setPreviewUrl(url);
    urlRef.current = url;
    return () => URL.revokeObjectURL(url);
  }, [file]);

  // Elapsed timer
  useEffect(() => {
    startRef.current = Date.now();
    setElapsed(0);
    if (!isPending) return;
    const id = setInterval(() => setElapsed(Date.now() - startRef.current), 100);
    return () => clearInterval(id);
  }, [isPending]);

  // Freeze elapsed on settle
  const [frozenElapsed, setFrozenElapsed] = useState<number | null>(null);
  useEffect(() => {
    if (isSuccess || isError) setFrozenElapsed(Date.now() - startRef.current);
  }, [isSuccess, isError]);

  const steps = deriveSteps(phase, progress, isPending, isSuccess, isError, errorMessage, file.name);

  const choice: ProgressTrackerChoice | undefined = isSuccess
    ? { outcome: "success", summary: "Uploaded", at: new Date().toISOString() }
    : isError
    ? { outcome: "failed", summary: "Upload failed", at: new Date().toISOString() }
    : undefined;

  const isVideo = file.type.startsWith("video/");
  const isImage = file.type.startsWith("image/");

  return (
    <div className="rounded-lg border bg-muted/30 overflow-hidden">
      {/* Media preview */}
      {previewUrl && (isImage || isVideo) && (
        <div className="relative w-full bg-black aspect-video overflow-hidden">
          {isImage ? (
            <img
              src={previewUrl}
              alt={file.name}
              className="w-full h-full object-contain"
            />
          ) : (
            <video
              src={previewUrl}
              className="w-full h-full object-contain"
              muted
              playsInline
              // ponytail: preload first frame only — no autoplay
              preload="metadata"
            />
          )}

          {/* Uploading overlay shimmer */}
          {isPending && (
            <div className="absolute inset-0 bg-black/50 flex flex-col items-center justify-center">
              {phase === "uploading" && (
                <div className="text-white font-medium text-lg mb-2">
                  {Math.round(progress)}%
                </div>
              )}
              <div className="w-full absolute bottom-0 h-1 bg-primary/20">
                <div
                  className="h-1 bg-primary transition-all duration-300"
                  style={{ width: `${phase === "uploading" ? progress : phase === "completing" ? 100 : 15}%` }}
                />
              </div>
            </div>
          )}

          {/* Success check */}
          {isSuccess && (
            <div className="absolute inset-0 flex items-center justify-center bg-black/20">
              <div className="w-12 h-12 rounded-full bg-green-500/90 flex items-center justify-center">
                <svg className="w-6 h-6 text-white" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={3}>
                  <path strokeLinecap="round" strokeLinejoin="round" d="M5 13l4 4L19 7" />
                </svg>
              </div>
            </div>
          )}
        </div>
      )}

      {/* Steps */}
      <div className="p-3">
        <ProgressTracker
          id={`upload-${file.name}`}
          steps={steps}
          elapsedTime={frozenElapsed ?? elapsed}
          choice={choice}
        />
      </div>
    </div>
  );
}
