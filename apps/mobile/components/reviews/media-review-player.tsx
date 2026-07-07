import React, { forwardRef, useImperativeHandle, useState, useEffect } from "react";
import { View, StyleSheet, Pressable, Text } from "react-native";
import { Image } from "expo-image";
import { useVideoPlayer, VideoView } from "expo-video";
import type { ReviewAsset, ReviewComment } from "@multica/core/types";
import { GestureDetector, Gesture } from "react-native-gesture-handler";
import Svg, { Rect } from "react-native-svg";
import { Ionicons } from "@expo/vector-icons";
import { MediaScrubber, formatTime, formatTimecode } from "./media-scrubber";

export interface MediaReviewPlayerProps {
  asset: ReviewAsset;
  onTimeUpdate?: (currentTime: number) => void;
  comments?: ReviewComment[];
  selectedCommentId?: string;
  onSelectComment?: (id: string) => void;
  onDrawingShapeChange?: (shape: any) => void;
}

export interface MediaReviewPlayerRef {
  seek: (time: number) => void;
  pause: () => void;
  getCanvasShapes: () => any;
  clearCanvasShapes: () => void;
  getCurrentTime: () => number;
}

export const MediaReviewPlayer = forwardRef<MediaReviewPlayerRef, MediaReviewPlayerProps>(
  ({ asset, onTimeUpdate, comments, selectedCommentId, onSelectComment, onDrawingShapeChange }, ref) => {
    const [drawingShape, setDrawingShape] = useState<any>(null);
    const [isDrawing, setIsDrawing] = useState(false);
    const [currentTime, setCurrentTime] = useState(0);
    const [isPlaying, setIsPlaying] = useState(false);

    // Only setup player if it's a video
    const player = useVideoPlayer(
      asset.asset_type === "video" ? asset.src_url : null,
      (player) => {
        player.loop = true;
      }
    );

    const [containerSize, setContainerSize] = useState({ width: 0, height: 0 });
    const [mediaSize, setMediaSize] = useState({ width: 0, height: 0 });
    
    const layout = React.useMemo(() => {
      if (!containerSize.width || !containerSize.height || !mediaSize.width || !mediaSize.height) {
        return { x: 0, y: 0, width: 0, height: 0 };
      }
      const containerAspect = containerSize.width / containerSize.height;
      const mediaAspect = mediaSize.width / mediaSize.height;
      let renderWidth = 0, renderHeight = 0, offsetX = 0, offsetY = 0;
      
      if (containerAspect > mediaAspect) {
        renderHeight = containerSize.height;
        renderWidth = renderHeight * mediaAspect;
        offsetX = (containerSize.width - renderWidth) / 2;
        offsetY = 0;
      } else {
        renderWidth = containerSize.width;
        renderHeight = renderWidth / mediaAspect;
        offsetX = 0;
        offsetY = (containerSize.height - renderHeight) / 2;
      }
      return { x: offsetX, y: offsetY, width: renderWidth, height: renderHeight };
    }, [containerSize, mediaSize]);

    // Track time and state
    useEffect(() => {
      if (!player) return;
      const subTime = player.addListener('timeUpdate', (event) => {
        const time = player.currentTime;
        setCurrentTime(time);
        onTimeUpdate?.(time);
        if (player.videoTrack?.size && (mediaSize.width === 0 || mediaSize.height === 0)) {
          setMediaSize(player.videoTrack.size);
        }
      });
      const subStatus = player.addListener('playingChange', (event) => {
        setIsPlaying(event.isPlaying);
      });
      if (player.videoTrack?.size) {
        setMediaSize(player.videoTrack.size);
      }
      return () => {
        subTime.remove();
        subStatus.remove();
      };
    }, [player, onTimeUpdate]);

    useImperativeHandle(ref, () => ({
      seek: (time: number) => {
        if (player) player.currentTime = time;
      },
      pause: () => {
        if (player) player.pause();
      },
      getCanvasShapes: () => {
        if (!drawingShape) return [];
        const shape = { ...drawingShape };
        if (shape.width < 0) { shape.x += shape.width; shape.width = Math.abs(shape.width); }
        if (shape.height < 0) { shape.y += shape.height; shape.height = Math.abs(shape.height); }
        if (shape.width < 0.01 && shape.height < 0.01) return [];
        return [shape];
      },
      clearCanvasShapes: () => {
        setDrawingShape(null);
        onDrawingShapeChange?.(null);
      },
      getCurrentTime: () => currentTime,
    }));

    const panGesture = Gesture.Pan()
      .onStart((e) => {
        if (!layout.width || !layout.height) return;
        // x and y in GestureDetector are relative to the bounding box of the Gesture.
        // But our Gesture is wrapped over a View that matches the screen, so we need to offset it.
        const relX = e.x - layout.x;
        const relY = e.y - layout.y;
        if (relX < 0 || relX > layout.width || relY < 0 || relY > layout.height) return;

        const nx = Math.max(0, Math.min(1, relX / layout.width));
        const ny = Math.max(0, Math.min(1, relY / layout.height));
        
        const color = ['#ef4444', '#f59e0b', '#10b981', '#3b82f6', '#a855f7', '#ec4899'][Math.floor(Math.random() * 6)];
        const newShape = { type: 'rectangle', x: nx, y: ny, width: 0, height: 0, color, strokeWidth: 2 };
        setDrawingShape(newShape);
        onDrawingShapeChange?.(newShape);
        
        if (player) player.pause();
      })
      .onUpdate((e) => {
        if (!drawingShape || !layout.width || !layout.height) return;
        const relX = e.x - layout.x;
        const relY = e.y - layout.y;
        const nx = Math.max(0, Math.min(1, relX / layout.width));
        const ny = Math.max(0, Math.min(1, relY / layout.height));
        const newShape = {
          ...drawingShape,
          width: nx - drawingShape.x,
          height: ny - drawingShape.y,
        };
        setDrawingShape(newShape);
        onDrawingShapeChange?.(newShape);
      });

    const visibleComments = (comments || []).filter(c => {
      if (asset.asset_type === 'image') return true;
      if (c.start_time !== null && c.start_time !== undefined && c.end_time !== null && c.end_time !== undefined) {
        if (c.start_time === c.end_time) {
          return Math.abs(currentTime - c.start_time) <= 0.25;
        }
        return currentTime >= c.start_time && currentTime <= c.end_time;
      }
      return false;
    });

    return (
      <View 
        style={styles.container}
        onLayout={(e) => {
          setContainerSize({ width: e.nativeEvent.layout.width, height: e.nativeEvent.layout.height });
        }}
      >
        {asset.asset_type === "video" && player ? (
          <VideoView 
            player={player} 
            style={StyleSheet.absoluteFill} 
            contentFit="contain"
            nativeControls={false}
          />
        ) : (
          <Image 
            source={asset.src_url} 
            style={StyleSheet.absoluteFill} 
            contentFit="contain" 
            onLoad={(e) => setMediaSize({ width: e.source.width, height: e.source.height })}
          />
        )}

        {layout.width > 0 && (
          <GestureDetector gesture={panGesture}>
            <View style={StyleSheet.absoluteFill}>
              <View 
                style={{
                  position: 'absolute',
                  left: layout.x,
                  top: layout.y,
                  width: layout.width,
                  height: layout.height,
                }}
              >
                <Svg style={StyleSheet.absoluteFill}>
                  {visibleComments.map(c => 
                    c.shapes?.map((s: any, i: number) => {
                      const isSelected = selectedCommentId === c.id;
                      const fillOpacity = isSelected ? 0.4 : 0.2;
                      return (
                        <Rect
                          key={`${c.id}-${i}`}
                          x={`${s.x * 100}%`}
                          y={`${s.y * 100}%`}
                          width={`${s.width * 100}%`}
                          height={`${s.height * 100}%`}
                          stroke={s.color}
                          strokeWidth={isSelected ? 4 : 2}
                          fill={s.color}
                          fillOpacity={fillOpacity}
                          onPress={() => onSelectComment?.(c.id)}
                        />
                      );
                    })
                  )}
                  {drawingShape && (
                    <Rect
                      x={`${Math.min(drawingShape.x, drawingShape.x + drawingShape.width) * 100}%`}
                      y={`${Math.min(drawingShape.y, drawingShape.y + drawingShape.height) * 100}%`}
                      width={`${Math.abs(drawingShape.width) * 100}%`}
                      height={`${Math.abs(drawingShape.height) * 100}%`}
                      stroke={drawingShape.color}
                      strokeWidth={2}
                      fill={drawingShape.color}
                      fillOpacity={0.3}
                    />
                  )}
                </Svg>
              </View>
            </View>
          </GestureDetector>
        )}

        {/* Custom Overlay Controls */}
        {asset.asset_type === "video" && player && (
          <View style={styles.controls} pointerEvents="box-none">
            <View style={styles.controlsInner}>
              <View style={styles.playControls}>
                <Pressable onPress={() => player.currentTime = Math.max(0, player.currentTime - 10)} style={styles.iconButton}>
                  <Ionicons name="play-back" size={20} color="white" />
                </Pressable>
                <Pressable onPress={() => { isPlaying ? player.pause() : player.play(); }} style={styles.playButton}>
                  <Ionicons name={isPlaying ? "pause" : "play"} size={24} color="black" />
                </Pressable>
                <Pressable onPress={() => player.currentTime = player.currentTime + 10} style={styles.iconButton}>
                  <Ionicons name="play-forward" size={20} color="white" />
                </Pressable>
                <Text style={styles.timeText}>{formatTimecode(currentTime)}</Text>
              </View>
              <MediaScrubber 
                currentTime={currentTime}
                duration={asset.duration || 0}
                comments={comments}
                selectedCommentId={selectedCommentId}
                onSeek={(time) => { player.currentTime = time; }}
                onSelectComment={onSelectComment}
                onScrubStart={() => player.pause()}
                onScrubEnd={() => { /* maybe resume */ }}
              />
            </View>
          </View>
        )}
      </View>
    );
  }
);

const styles = StyleSheet.create({
  container: {
    flex: 1,
    width: "100%",
    backgroundColor: "#000",
    overflow: "hidden",
  },
  controls: {
    ...StyleSheet.absoluteFillObject,
    justifyContent: "flex-end",
    paddingBottom: 24,
    paddingHorizontal: 16,
  },
  controlsInner: {
    backgroundColor: 'rgba(0,0,0,0.6)',
    borderRadius: 16,
    padding: 12,
  },
  playControls: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 16,
    marginBottom: 8,
  },
  iconButton: {
    padding: 8,
  },
  playButton: {
    width: 44,
    height: 44,
    borderRadius: 22,
    backgroundColor: 'white',
    alignItems: 'center',
    justifyContent: 'center',
  },
  timeText: {
    color: 'white',
    fontFamily: 'Courier',
    position: 'absolute',
    right: 8,
  }
});
