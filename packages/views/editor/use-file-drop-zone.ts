import { useState, useEffect, useCallback, useRef, type DragEvent } from "react";

interface UseFileDropZoneOptions {
  onDrop: (files: File[]) => void;
  enabled?: boolean;
}

function useFileDropZone({ onDrop, enabled = true }: UseFileDropZoneOptions) {
  const [isDragOver, setIsDragOver] = useState(false);
  const onDropRef = useRef(onDrop);
  const dragDepthRef = useRef(0);
  onDropRef.current = onDrop;

  // Clear on any document-level drop or dragend (e.g. user drops outside the zone)
  useEffect(() => {
    if (!enabled) return;
    const clear = () => {
      dragDepthRef.current = 0;
      setIsDragOver(false);
    };
    document.addEventListener("drop", clear, true);
    document.addEventListener("dragend", clear, true);
    return () => {
      document.removeEventListener("drop", clear, true);
      document.removeEventListener("dragend", clear, true);
    };
  }, [enabled]);

  useEffect(() => {
    if (!enabled) {
      dragDepthRef.current = 0;
      setIsDragOver(false);
    }
  }, [enabled]);

  const isFileDrag = useCallback((e: DragEvent) => {
    const types = e.dataTransfer?.types;
    if (!types) return false;
    return Array.from(types as Iterable<string>).includes("Files");
  }, []);

  const handleDragEnter = useCallback(
    (e: DragEvent) => {
      if (enabled && isFileDrag(e)) {
        e.preventDefault();
        dragDepthRef.current += 1;
        setIsDragOver(true);
      }
    },
    [enabled, isFileDrag],
  );

  const handleDragOver = useCallback((e: DragEvent) => {
    if (!enabled || !isFileDrag(e)) return;
    e.preventDefault();
  }, [enabled, isFileDrag]);

  const handleDragLeave = useCallback(
    (e: DragEvent) => {
      if (!enabled) return;
      e.preventDefault();
      
      dragDepthRef.current = Math.max(0, dragDepthRef.current - 1);
      if (dragDepthRef.current === 0) {
        setIsDragOver(false);
      }
    },
    [enabled],
  );

  const handleDrop = useCallback(
    (e: DragEvent) => {
      if (!enabled || !isFileDrag(e)) return;

      const target = e.target as HTMLElement | null;
      
      // Reset state immediately
      dragDepthRef.current = 0;
      setIsDragOver(false);

      // If dropping directly into the editor content area, let Tiptap handle it
      // to preserve the drop position accuracy. External drop zone only handles 
      // drops on the wrapper/background.
      if (target?.closest(".ProseMirror")) {
        return;
      }

      const alreadyHandled = e.nativeEvent.defaultPrevented;
      e.preventDefault();

      if (alreadyHandled) return;

      const files = e.dataTransfer?.files;
      if (files?.length) {
        onDropRef.current(Array.from(files));
      }
    },
    [enabled, isFileDrag],
  );

  const dropZoneProps = {
    onDragEnter: handleDragEnter,
    onDragOver: handleDragOver,
    onDragLeave: handleDragLeave,
    onDrop: handleDrop,
  };

  return { isDragOver: enabled && isDragOver, dropZoneProps };
}

export { useFileDropZone };
