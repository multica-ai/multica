import React from "react";
import { View, StyleSheet, Pressable } from "react-native";
import { Ionicons } from "@expo/vector-icons";

export interface ToolPaletteProps {
  selectedTool: 'pen' | 'arrow' | 'rectangle';
  onSelectTool: (tool: 'pen' | 'arrow' | 'rectangle') => void;
  selectedColor: string;
  onSelectColor: (color: string) => void;
  onClear: () => void;
}

const COLORS = ['#ef4444', '#f59e0b', '#10b981', '#3b82f6', '#a855f7', '#ec4899'];

export function ToolPalette({ selectedTool, onSelectTool, selectedColor, onSelectColor, onClear }: ToolPaletteProps) {
  return (
    <View style={styles.container}>
      <View style={styles.tools}>
        <Pressable 
          onPress={() => onSelectTool('pen')} 
          style={[styles.toolButton, selectedTool === 'pen' && styles.toolButtonSelected]}
        >
          <Ionicons name="pencil" size={20} color={selectedTool === 'pen' ? "#3b82f6" : "white"} />
        </Pressable>
        <Pressable 
          onPress={() => onSelectTool('arrow')} 
          style={[styles.toolButton, selectedTool === 'arrow' && styles.toolButtonSelected]}
        >
          <Ionicons name="arrow-forward" size={20} color={selectedTool === 'arrow' ? "#3b82f6" : "white"} />
        </Pressable>
        <Pressable 
          onPress={() => onSelectTool('rectangle')} 
          style={[styles.toolButton, selectedTool === 'rectangle' && styles.toolButtonSelected]}
        >
          <Ionicons name="stop-outline" size={20} color={selectedTool === 'rectangle' ? "#3b82f6" : "white"} />
        </Pressable>
      </View>
      
      <View style={styles.divider} />

      <View style={styles.colors}>
        {COLORS.map(c => (
          <Pressable
            key={c}
            onPress={() => onSelectColor(c)}
            style={[
              styles.colorSwatch,
              { backgroundColor: c },
              selectedColor === c && styles.colorSwatchSelected
            ]}
          />
        ))}
      </View>

      <View style={styles.divider} />

      <Pressable onPress={onClear} style={styles.toolButton}>
        <Ionicons name="trash-outline" size={20} color="#ef4444" />
      </Pressable>
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    flexDirection: "column",
    backgroundColor: "rgba(0,0,0,0.7)",
    borderRadius: 24,
    padding: 8,
    gap: 8,
    alignItems: "center",
  },
  tools: {
    gap: 8,
  },
  colors: {
    gap: 8,
  },
  divider: {
    height: 1,
    width: "80%",
    backgroundColor: "rgba(255,255,255,0.2)",
  },
  toolButton: {
    width: 40,
    height: 40,
    borderRadius: 20,
    alignItems: "center",
    justifyContent: "center",
  },
  toolButtonSelected: {
    backgroundColor: "rgba(59, 130, 246, 0.2)",
  },
  colorSwatch: {
    width: 24,
    height: 24,
    borderRadius: 12,
    borderWidth: 2,
    borderColor: "transparent",
  },
  colorSwatchSelected: {
    borderColor: "white",
  },
});
