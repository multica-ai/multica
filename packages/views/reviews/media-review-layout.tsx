import { useRef, useState } from "react";
import { ReviewAsset } from "@multica/core/types";
import { MediaReviewPlayer, type MediaReviewPlayerRef } from "./media-review-player";
import { ReviewCommentSidebar } from "./review-comment-sidebar";

interface MediaReviewLayoutProps {
  workspaceId: string;
  asset: ReviewAsset;
}

export function MediaReviewLayout({ workspaceId, asset }: MediaReviewLayoutProps) {
  const playerRef = useRef<MediaReviewPlayerRef>(null);
  const [currentTime, setCurrentTime] = useState(0);

  const handleSeek = (time: number) => {
    playerRef.current?.seek(time);
  };

  const handleDrawStart = () => {
    // When user focuses textarea, pause the video so they can draw
    playerRef.current?.pause();
  };

  const getCanvasShapes = () => {
    return playerRef.current?.getCanvasShapes();
  };

  const clearCanvasShapes = () => {
    playerRef.current?.clearCanvasShapes();
  };

  return (
    <div className="flex h-full w-full bg-black">
      <div className="flex-1 relative">
        <MediaReviewPlayer 
          ref={playerRef} 
          asset={asset} 
          onTimeUpdate={setCurrentTime}
        />
      </div>
      <div className="w-80 h-full border-l border-gray-800 bg-white">
        <ReviewCommentSidebar
          workspaceId={workspaceId}
          asset={asset}
          currentTime={currentTime}
          onSeek={handleSeek}
          onDrawStart={handleDrawStart}
          getCanvasShapes={getCanvasShapes}
          clearCanvasShapes={clearCanvasShapes}
        />
      </div>
    </div>
  );
}
