import { useEffect, useMemo, useState } from "react";
import {
  ActivityIndicator,
  Alert,
  Image,
  Modal,
  NativeModules,
  Platform,
  Pressable,
  StatusBar,
  StyleSheet,
  useWindowDimensions,
  View,
} from "react-native";
import * as FileSystem from "expo-file-system/legacy";
import * as MediaLibrary from "expo-media-library";
import { useTranslation } from "react-i18next";
import { GestureHandlerRootView } from "react-native-gesture-handler";
import Animated, {
  Extrapolation,
  interpolate,
  useAnimatedStyle,
  useSharedValue,
  withSpring,
} from "react-native-reanimated";
import { scheduleOnRN } from "react-native-worklets";
import Svg, { Path } from "react-native-svg";
import { Gallery, type VerticalPullOptions } from "react-native-zoom-toolkit";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { spacing } from "../../theme/tokens";

export type PreviewImageItem = {
  contentType?: string;
  filename?: string;
  id: string;
  uri: string;
};

const FullscreenStatusBar = NativeModules.FullscreenStatusBar as
  | { hide?: () => void; show?: () => void }
  | undefined;

export function ImagePreviewModal({
  images,
  initialIndex,
  onClose,
  open,
}: {
  images: PreviewImageItem[];
  initialIndex: number;
  onClose: () => void;
  open: boolean;
}) {
  const { t } = useTranslation();
  const insets = useSafeAreaInsets();
  const { height: windowHeight, width: windowWidth } = useWindowDimensions();
  const [currentImageIndex, setCurrentImageIndex] = useState(initialIndex);
  const [saveState, setSaveState] = useState<"idle" | "saving">("idle");
  const [controlsVisible, setControlsVisible] = useState(true);
  const pullDistance = useSharedValue(0);
  const pullStyle = useAnimatedStyle(() => {
    const distance = Math.min(Math.max(pullDistance.value, 0), 220);

    return {
      opacity: interpolate(distance, [0, 220], [1, 0.72], Extrapolation.CLAMP),
      transform: [
        { scale: interpolate(distance, [0, 220], [1, 0.88], Extrapolation.CLAMP) },
      ],
    };
  });

  const safeInitialIndex = useMemo(
    () => Math.max(0, Math.min(initialIndex, Math.max(0, images.length - 1))),
    [images.length, initialIndex],
  );

  useEffect(() => {
    setCurrentImageIndex(safeInitialIndex);
    setControlsVisible(true);
  }, [safeInitialIndex]);

  useEffect(() => {
    if (!open) setSaveState("idle");
  }, [open]);

  useEffect(() => {
    if (open) {
      pullDistance.value = 0;
    }
  }, [open, pullDistance, safeInitialIndex]);

  useEffect(() => {
    if (!open) return undefined;

    StatusBar.setHidden(true, "fade");
    if (Platform.OS === "android") FullscreenStatusBar?.hide?.();

    return () => {
      if (Platform.OS === "android") FullscreenStatusBar?.show?.();
      StatusBar.setHidden(false, "fade");
    };
  }, [open]);

  async function saveImage(imageIndex = currentImageIndex) {
    const image = images[imageIndex];
    if (!image || saveState === "saving") return;
    setSaveState("saving");
    try {
      const permission = await MediaLibrary.requestPermissionsAsync();
      if (!permission.granted) {
        Alert.alert(t("issues.save_image_permission_title"), t("issues.save_image_permission_body"));
        return;
      }
      const extension = getImageFileExtension(image) || "jpg";
      const localUri = `${FileSystem.cacheDirectory ?? ""}${image.id}.${extension}`;
      const result = await FileSystem.downloadAsync(image.uri, localUri);
      await MediaLibrary.saveToLibraryAsync(result.uri);
      Alert.alert(t("issues.save_image_success_title"), t("issues.save_image_success_body"));
    } catch (err) {
      Alert.alert(
        t("issues.save_image_failed_title"),
        err instanceof Error ? err.message : t("issues.save_image_failed_body"),
      );
    } finally {
      setSaveState("idle");
    }
  }

  function handleVerticalPull({ translateY, released, velocityY }: VerticalPullOptions) {
    "worklet";
    const downwardTranslateY = Math.max(translateY, 0);
    pullDistance.value = downwardTranslateY;
    if (!released) return;
    const shouldClose = downwardTranslateY > 110 || (downwardTranslateY > 35 && velocityY > 800);
    if (shouldClose) {
      scheduleOnRN(onClose);
      return;
    }
    pullDistance.value = withSpring(0, { damping: 18, stiffness: 220 });
  }

  return (
    <Modal
      animationType="fade"
      onRequestClose={onClose}
      presentationStyle="fullScreen"
      statusBarTranslucent
      visible={open}
    >
      <StatusBar backgroundColor="transparent" barStyle="light-content" hidden translucent />
      <GestureHandlerRootView style={styles.root}>
        <Animated.View style={[styles.gallery, pullStyle]}>
          <Gallery
            data={images}
            gap={24}
            initialIndex={safeInitialIndex}
            keyExtractor={(item) => item.id}
            maxScale={4}
            onIndexChange={setCurrentImageIndex}
            onTap={() => setControlsVisible((visible) => !visible)}
            onVerticalPull={handleVerticalPull}
            pinchMode="clamp"
            renderItem={(item) => (
              <View style={{ height: windowHeight, width: windowWidth }}>
                <Image
                  resizeMode="contain"
                  source={{ uri: item.uri }}
                  style={styles.image}
                />
              </View>
            )}
            scaleMode="clamp"
            tapOnEdgeToItem={false}
            windowSize={3}
          />
        </Animated.View>

        {controlsVisible ? (
          <View pointerEvents="box-none" style={StyleSheet.absoluteFill}>
            <Pressable
              accessibilityLabel={t("issues.save_image")}
              accessibilityRole="button"
              disabled={saveState === "saving"}
              onPress={() => void saveImage(currentImageIndex)}
              style={({ pressed }) => [
                styles.saveButton,
                {
                  bottom: Math.max(insets.bottom, spacing.lg),
                  right: Math.max(insets.right, spacing.lg),
                },
                saveState === "saving" && styles.disabled,
                pressed && saveState !== "saving" && styles.actionPressed,
              ]}
            >
              {saveState === "saving" ? (
                <ActivityIndicator color="#fff" size="small" />
              ) : (
                <DownloadIcon />
              )}
            </Pressable>
          </View>
        ) : null}
      </GestureHandlerRootView>
    </Modal>
  );
}

function DownloadIcon() {
  return (
    <Svg fill="none" height={24} viewBox="0 0 24 24" width={24}>
      <Path
        d="M12 3v11m0 0 4-4m-4 4-4-4M5 17v2a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2v-2"
        stroke="#fff"
        strokeLinecap="round"
        strokeLinejoin="round"
        strokeWidth={2.2}
      />
    </Svg>
  );
}

function getImageFileExtension(image: PreviewImageItem) {
  const filenameExtension = image.filename?.match(/\.([a-z0-9]+)$/i)?.[1];
  if (filenameExtension) return filenameExtension.toLowerCase();
  return image.contentType?.split("/")[1]?.split(";")[0]?.toLowerCase() ?? "";
}

const styles = StyleSheet.create({
  root: {
    backgroundColor: "#000",
    flex: 1,
  },
  gallery: {
    flex: 1,
  },
  image: {
    height: "100%",
    width: "100%",
  },
  saveButton: {
    alignItems: "center",
    backgroundColor: "rgba(0, 0, 0, 0.48)",
    borderRadius: 26,
    height: 52,
    justifyContent: "center",
    position: "absolute",
    width: 52,
  },
  actionPressed: {
    backgroundColor: "rgba(255, 255, 255, 0.18)",
  },
  disabled: {
    opacity: 0.55,
  },
});
