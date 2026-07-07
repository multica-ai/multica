import React, { useState } from "react";
import { View, StyleSheet, TextInput, Pressable, KeyboardAvoidingView, Platform, Text } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useSafeAreaInsets } from "react-native-safe-area-context";

export interface ReviewComposerProps {
  timestamp?: number | null;
  onSend: (content: string) => void;
  onCancel: () => void;
}

function formatTimecode(seconds: number): string {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = Math.floor(seconds % 60);
  if (h > 0) return `${h}:${m.toString().padStart(2, "0")}:${s.toString().padStart(2, "0")}`;
  return `${m}:${s.toString().padStart(2, "0")}`;
}

export function ReviewComposer({ timestamp, onSend, onCancel }: ReviewComposerProps) {
  const [content, setContent] = useState("");
  const insets = useSafeAreaInsets();

  return (
    <KeyboardAvoidingView 
      style={styles.container} 
      behavior={Platform.OS === "ios" ? "padding" : "height"}
    >
      <View style={[styles.inner, { paddingBottom: Math.max(insets.bottom, 12) }]}>
        <View style={styles.header}>
          <Text style={styles.title}>
            {timestamp !== undefined && timestamp !== null 
              ? `Comment at ${formatTimecode(timestamp)}` 
              : "Add a comment"}
          </Text>
          <Pressable onPress={onCancel} style={styles.closeButton}>
            <Ionicons name="close" size={20} color="#666" />
          </Pressable>
        </View>
        <View style={styles.inputContainer}>
          <TextInput
            style={styles.input}
            placeholder="Type your comment..."
            placeholderTextColor="#999"
            multiline
            autoFocus
            value={content}
            onChangeText={setContent}
          />
          <Pressable 
            style={[styles.sendButton, !content.trim() && styles.sendButtonDisabled]}
            disabled={!content.trim()}
            onPress={() => {
              if (content.trim()) onSend(content);
            }}
          >
            <Ionicons name="send" size={16} color="white" />
          </Pressable>
        </View>
      </View>
    </KeyboardAvoidingView>
  );
}

const styles = StyleSheet.create({
  container: {
    position: "absolute",
    bottom: 0,
    left: 0,
    right: 0,
    backgroundColor: "white",
    borderTopLeftRadius: 16,
    borderTopRightRadius: 16,
    shadowColor: "#000",
    shadowOffset: { width: 0, height: -2 },
    shadowOpacity: 0.1,
    shadowRadius: 8,
    elevation: 10,
    zIndex: 100,
  },
  inner: {
    padding: 16,
  },
  header: {
    flexDirection: "row",
    justifyContent: "space-between",
    alignItems: "center",
    marginBottom: 12,
  },
  title: {
    fontSize: 14,
    fontWeight: "600",
    color: "#333",
  },
  closeButton: {
    padding: 4,
  },
  inputContainer: {
    flexDirection: "row",
    alignItems: "flex-end",
    backgroundColor: "#f4f4f5",
    borderRadius: 20,
    paddingHorizontal: 12,
    paddingVertical: 8,
  },
  input: {
    flex: 1,
    minHeight: 36,
    maxHeight: 120,
    fontSize: 16,
    color: "#000",
    paddingTop: 8,
    paddingBottom: 8,
  },
  sendButton: {
    width: 32,
    height: 32,
    borderRadius: 16,
    backgroundColor: "#3b82f6",
    justifyContent: "center",
    alignItems: "center",
    marginLeft: 8,
    marginBottom: 2,
  },
  sendButtonDisabled: {
    backgroundColor: "#a1a1aa",
  },
});
