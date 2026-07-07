import React, { forwardRef, useImperativeHandle, useState, useEffect } from "react";
import { View, StyleSheet, Pressable, Text } from "react-native";
import { Image } from "expo-image";
import { useVideoPlayer, VideoView } from "expo-video";
import type { ReviewAsset, ReviewComment } from "@multica/core/types";
import { GestureDetector, Gesture } from "react-native-gesture-handler";
import Svg, { Rect, Path, Line, Polygon, Ellipse } from "react-native-svg";
import { Ionicons } from "@expo/vector-icons";
import { MediaScrubber, formatTimecode } from "./media-scrubber";
import {
  normalizeShape,
  rectToSvgProps,
  ellipseToSvgProps,
  pointsToSvgPath,
  arrowHeadPoints,
  type ReviewShape,
} from "@/lib/review-shape-geometry";

export interface MediaReviewPlayerProps {
  asset: ReviewAsset;
  onTimeUpdate?: (currentTime: number) => void;
  comments?: ReviewComment[];
  selectedCommentId?: string;
  onSelectComment?: (id: string) => void;
  onDrawingShapeChange?: (shape: any) => void;
  selectedTool?: 'pen' | 'arrow' | 'rectangle' | 'ellipse';
  selectedColor?: string;
}

export interface MediaReviewPlayerRef {
  seek: (time: number) => void;
  pause: () => void;
  getCanvasShapes: () => any;
  clearCanvasShapes: () => void;
  getCurrentTime: () => number;
}

/**
 * Render one annotation shape in px coordinates against the current media
 * layout. All coordinates go through lib/review-shape-geometry so Path/
 * Polygon (which have no "%" form) and Rect/Ellipse use the same units.
 */
function renderShape(
  s: ReviewShape,
  w: number,
  h: number,
  opts: { key: string; strokeWidth: number; fillOpacity: number; onPress?: () => void },
) {
  const { key, strokeWidth, fillOpacity, onPress } = opts;
  if (s.type === "pen" && s.points && s.points.length > 0) {
    return (
      <Path
        key={key}
        d={pointsToSvgPath(s.points, w, h)}
        stroke={s.color}
        strokeWidth={strokeWidth}
        fill="none"
        strokeLinecap="round"
        strokeLinejoin="round"
        onPress={onPress}
      />
    );
  }
  if (s.type === "arrow" && s.points && s.points.length === 2) {
    const [start, end] = s.points as [
      { x: number; y: number },
      { x: number; y: number },
    ];
    return (
      <React.Fragment key={key}>
        <Line
          x1={start.x * w}
          y1={start.y * h}
          x2={end.x * w}
          y2={end.y * h}
          stroke={s.color}
          strokeWidth={strokeWidth}
          onPress={onPress}
        />
        <Polygon points={arrowHeadPoints(start, end, w, h)} fill={s.color} onPress={onPress} />
      </React.Fragment>
    );
  }
  if (s.type === "ellipse") {
    return (
      <Ellipse
        key={key}
        {...ellipseToSvgProps(s, w, h)}
        stroke={s.color}
        strokeWidth={strokeWidth}
        fill={s.color}
        fillOpacity={fillOpacity}
        onPress={onPress}
      />
    );
  }
  return (
    <Rect
      key={key}
      {...rectToSvgProps(s, w, h)}
      stroke={s.color}
      strokeWidth={strokeWidth}
      fill={s.color}
      fillOpacity={fillOpacity}
      onPress={onPress}
    />
  );
}

export const MediaReviewPlayer = forwardRef<MediaReviewPlayerRef, MediaReviewPlayerProps>(
  ({ asset, onTimeUpdate, comments, selectedCommentId, onSelectComment, onDrawingShapeChange, selectedTool = 'rectangle', selectedColor = '#ef4444' }, ref) => {
    const [drawingShape, setDrawingShape] = useState<ReviewShape | null>(null);
    const [currentTime, setCurrentTime] = useState(0);
    const [duration, setDuration] = useState(0);
    const [isPlaying, setIsPlaying] = useState(false);

    // Only setup player if it's a video
    const player = useVideoPlayer(
      asset.asset_type === "video" ? asset.src_url : null,
      (player) => {
        player.loop = true;
        // timeUpdate events are DISABLED by default (interval 0) — without
        // this line currentTime never advances, so the scrubber freezes and
        // timed comment overlays never appear.
        player.timeUpdateEventInterval = 0.25;
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
        // Duration comes from the loaded item, not the asset row — the
        // backend doesn't store duration for every upload, and a 0 duration
        // makes every scrubber seek collapse to t=0.
        if (Number.isFinite(player.duration) && player.duration > 0) {
          setDuration((d) => (d === player.duration ? d : player.duration));
        }
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
        const shape = normalizeShape(drawingShape);
        return shape ? [shape] : [];
      },
      clearCanvasShapes: () => {
        setDrawingShape(null);
        onDrawingShapeChange?.(null);
      },
      getCurrentTime: () => currentTime,
    }));

    // runOnJS(true): the callbacks below call React setState and parent
    // callbacks. Without this flag gesture-handler runs them as Reanimated
    // worklets on the UI thread, which crashes on the first touch ("Tried
    // to synchronously call a non-worklet function on the UI thread").
    // Drawing is setState-driven anyway, so JS-thread gestures cost nothing.
    const panGesture = Gesture.Pan()
      .runOnJS(true)
      .onStart((e) => {
        if (!layout.width || !layout.height) return;
        // x and y in GestureDetector are relative to the bounding box of the Gesture.
        // But our Gesture is wrapped over a View that matches the screen, so we need to offset it.
        const relX = e.x - layout.x;
        const relY = e.y - layout.y;
        if (relX < 0 || relX > layout.width || relY < 0 || relY > layout.height) return;

        const nx = Math.max(0, Math.min(1, relX / layout.width));
        const ny = Math.max(0, Math.min(1, relY / layout.height));
        
        // Point shapes still carry zeroed x/y/width/height — core's
        // AnnotationShape declares them required for every type.
        const newShape: ReviewShape = (selectedTool === 'rectangle' || selectedTool === 'ellipse')
          ? { type: selectedTool, x: nx, y: ny, width: 0, height: 0, color: selectedColor, strokeWidth: 2 }
          : { type: selectedTool, x: 0, y: 0, width: 0, height: 0, points: [{x: nx, y: ny}, {x: nx, y: ny}], color: selectedColor, strokeWidth: 2 };
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
        
        let newShape: ReviewShape = drawingShape;
        if (drawingShape.type === 'rectangle' || drawingShape.type === 'ellipse') {
          newShape = {
            ...drawingShape,
            width: nx - (drawingShape.x ?? 0),
            height: ny - (drawingShape.y ?? 0),
          };
        } else if (drawingShape.type === 'pen' && drawingShape.points?.length) {
          const lastPoint = drawingShape.points[drawingShape.points.length - 1]!;
          const dist = Math.hypot(lastPoint.x - nx, lastPoint.y - ny);
          if (dist > 0.005) {
            newShape = {
              ...drawingShape,
              points: [...drawingShape.points, {x: nx, y: ny}]
            };
          }
        } else if (drawingShape.type === 'arrow' && drawingShape.points?.length) {
          newShape = {
            ...drawingShape,
            points: [drawingShape.points[0]!, {x: nx, y: ny}]
          };
        }
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
                    c.shapes?.map((s: ReviewShape, i: number) => {
                      const isSelected = selectedCommentId === c.id;
                      const fillOpacity = isSelected ? 0.4 : 0.2;
                      const strokeWidth = isSelected ? 4 : 2;
                      const select = () => onSelectComment?.(c.id);
                      return renderShape(s, layout.width, layout.height, {
                        key: `${c.id}-${i}`,
                        strokeWidth,
                        fillOpacity,
                        onPress: select,
                      });
                    })
                  )}
                  {drawingShape &&
                    renderShape(drawingShape, layout.width, layout.height, {
                      key: "drawing",
                      strokeWidth: 2,
                      fillOpacity: 0.3,
                    })}
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
                duration={duration || asset.duration || 0}
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

MediaReviewPlayer.displayName = "MediaReviewPlayer";

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
