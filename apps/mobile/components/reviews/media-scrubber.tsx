import React, { useCallback, useState } from "react";
import { View, StyleSheet, Text, LayoutChangeEvent, Pressable } from "react-native";
import { GestureDetector, Gesture } from "react-native-gesture-handler";
import Animated, { useAnimatedStyle, useSharedValue, withTiming } from "react-native-reanimated";
import type { ReviewComment } from "@multica/core/types";
import { ActorAvatar } from "../ui/actor-avatar";

export function formatTimecode(seconds: number): string {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = Math.floor(seconds % 60);
  const ms = Math.floor((seconds % 1) * 100);
  if (h > 0) return `${h}:${m.toString().padStart(2, "0")}:${s.toString().padStart(2, "0")}`;
  return `${m}:${s.toString().padStart(2, "0")}.${ms.toString().padStart(2, "0")}`;
}

export function formatTime(seconds: number): string {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = Math.floor(seconds % 60);
  if (h > 0) return `${h}:${m.toString().padStart(2, "0")}:${s.toString().padStart(2, "0")}`;
  return `${m}:${s.toString().padStart(2, "0")}`;
}

export function formatFrames(seconds: number, fps = 30): string {
  return Math.floor(seconds * fps).toString();
}

export interface MediaScrubberProps {
  currentTime: number;
  duration: number;
  buffered?: number;
  comments?: ReviewComment[];
  selectedCommentId?: string;
  onSeek: (time: number) => void;
  onSelectComment?: (id: string) => void;
  onScrubStart?: () => void;
  onScrubEnd?: () => void;
}

export function MediaScrubber({
  currentTime,
  duration,
  buffered = 0,
  comments = [],
  selectedCommentId,
  onSeek,
  onSelectComment,
  onScrubStart,
  onScrubEnd,
}: MediaScrubberProps) {
  const [trackWidth, setTrackWidth] = useState(0);

  const timeToPercent = useCallback(
    (time: number): number => {
      if (!duration) return 0;
      return Math.max(0, Math.min(100, (time / duration) * 100));
    },
    [duration]
  );

  const handleLayout = (e: LayoutChangeEvent) => {
    setTrackWidth(e.nativeEvent.layout.width);
  };

  const panGesture = Gesture.Pan()
    .onStart((e) => {
      if (!trackWidth) return;
      if (onScrubStart) onScrubStart();
      const ratio = Math.max(0, Math.min(1, e.x / trackWidth));
      onSeek(ratio * duration);
    })
    .onUpdate((e) => {
      if (!trackWidth) return;
      const ratio = Math.max(0, Math.min(1, e.x / trackWidth));
      onSeek(ratio * duration);
    })
    .onEnd(() => {
      if (onScrubEnd) onScrubEnd();
    });

  const tapGesture = Gesture.Tap()
    .onEnd((e) => {
      if (!trackWidth) return;
      const ratio = Math.max(0, Math.min(1, e.x / trackWidth));
      onSeek(ratio * duration);
    });

  const composedGesture = Gesture.Exclusive(panGesture, tapGesture);

  const playPercent = timeToPercent(currentTime);
  const bufferedPercent = timeToPercent(buffered);

  const pointMarkers = comments.filter((c) => c.start_time !== null && c.start_time !== undefined && (c.end_time === null || c.end_time === undefined || c.start_time === c.end_time));
  const rangeMarkers = comments.filter((c) => c.start_time !== null && c.start_time !== undefined && c.end_time !== null && c.end_time !== undefined && c.start_time !== c.end_time);

  return (
    <View style={styles.container}>
      <GestureDetector gesture={composedGesture}>
        <View style={styles.touchArea} onLayout={handleLayout}>
          <View style={styles.trackBackground} />
          
          <View style={[styles.trackBuffered, { width: `${bufferedPercent}%` }]} />
          
          {rangeMarkers.map((c) => {
            if (c.start_time === null || c.start_time === undefined || c.end_time === null || c.end_time === undefined) return null;
            const left = timeToPercent(c.start_time);
            const right = timeToPercent(c.end_time);
            const isSelected = selectedCommentId === c.id;
            return (
              <View
                key={c.id}
                style={[
                  styles.rangeMarker,
                  { left: `${left}%`, width: `${right - left}%` },
                  isSelected ? { backgroundColor: 'rgba(59, 130, 246, 0.6)' } : { backgroundColor: 'rgba(59, 130, 246, 0.3)' }
                ]}
              />
            );
          })}

          <View style={[styles.trackFill, { width: `${playPercent}%` }]} />
          
          <Animated.View style={[styles.thumb, { left: `${playPercent}%` }]} />
        </View>
      </GestureDetector>

      {pointMarkers.length > 0 && (
        <View style={styles.markerContainer}>
          {pointMarkers.map((c) => {
            if (c.start_time === null || c.start_time === undefined) return null;
            const left = timeToPercent(c.start_time);
            const color = c.shapes?.[0]?.color || (c.resolved ? "#22c55e" : "#3b82f6");
            const isSelected = selectedCommentId === c.id;

            return (
              <Pressable
                key={c.id}
                style={[styles.marker, { left: `${left}%`, borderColor: isSelected ? color : 'transparent' }]}
                onPress={() => {
                  if (c.start_time !== undefined) onSeek(c.start_time);
                  onSelectComment?.(c.id);
                }}
                hitSlop={{ top: 10, bottom: 10, left: 10, right: 10 }}
              >
                <View style={[styles.markerInner, { backgroundColor: color, transform: [{ scale: isSelected ? 1.2 : 1 }] }]}>
                  <ActorAvatar type="member" id={c.author_id} size={16} />
                </View>
              </Pressable>
            );
          })}
        </View>
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    width: "100%",
    paddingVertical: 12,
  },
  touchArea: {
    height: 32,
    justifyContent: "center",
    position: "relative",
  },
  trackBackground: {
    position: "absolute",
    left: 0,
    right: 0,
    height: 6,
    backgroundColor: "rgba(255, 255, 255, 0.2)",
    borderRadius: 3,
  },
  trackBuffered: {
    position: "absolute",
    left: 0,
    height: 6,
    backgroundColor: "rgba(255, 255, 255, 0.3)",
    borderRadius: 3,
  },
  trackFill: {
    position: "absolute",
    left: 0,
    height: 6,
    backgroundColor: "#3b82f6",
    borderRadius: 3,
  },
  rangeMarker: {
    position: "absolute",
    height: 6,
    borderRadius: 3,
  },
  thumb: {
    position: "absolute",
    width: 16,
    height: 16,
    borderRadius: 8,
    backgroundColor: "#3b82f6",
    transform: [{ translateX: -8 }],
    shadowColor: "#000",
    shadowOffset: { width: 0, height: 2 },
    shadowOpacity: 0.5,
    shadowRadius: 4,
    elevation: 4,
  },
  markerContainer: {
    position: "relative",
    height: 24,
    width: "100%",
    marginTop: 4,
  },
  marker: {
    position: "absolute",
    top: 0,
    width: 24,
    height: 24,
    transform: [{ translateX: -12 }],
    justifyContent: "center",
    alignItems: "center",
    borderWidth: 2,
    borderRadius: 12,
  },
  markerInner: {
    width: 20,
    height: 20,
    borderRadius: 10,
    overflow: "hidden",
    justifyContent: "center",
    alignItems: "center",
  },
});
