import { useRef } from "react";
import { FolderOpen } from "lucide-react";
import { Input } from "@multica/ui/components/ui/input";

interface DirInputProps {
  value: string | null;
  inputKey?: string | number;
  className?: string;
  onCommit: (dir: string | null) => void;
}

/**
 * A text input for filesystem paths with a browse button.
 * Uses showDirectoryPicker() — opens a native OS directory picker
 * without enumerating any files inside the selected folder.
 * In Electron, the handle may expose a full absolute path via _path.
 * In a browser, falls back to the directory name; the user can type the
 * full path manually.
 */
export function DirInput({ value, inputKey, className, onCommit }: DirInputProps) {
  const inputRef = useRef<HTMLInputElement>(null);

  const handleBrowse = async () => {
    try {
      // showDirectoryPicker opens a native picker with no file enumeration.
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const picker = (window as any).showDirectoryPicker;
      if (!picker) return;
      const handle = await picker({ mode: "read" });
      // Electron exposes the real absolute path via handle._path; browsers
      // only expose the folder name via handle.name.
      const dir: string = handle._path ?? handle.name;
      if (inputRef.current) inputRef.current.value = dir;
      onCommit(dir || null);
    } catch {
      // User cancelled the picker — do nothing.
    }
  };

  return (
    <div className={`flex items-center gap-1 ${className ?? ""}`}>
      <div className="relative flex-1 min-w-0">
        <FolderOpen className="absolute left-1.5 top-1/2 -translate-y-1/2 h-3 w-3 text-muted-foreground pointer-events-none" />
        <Input
          ref={inputRef}
          key={inputKey}
          className="h-6 text-xs font-mono pl-5 pr-1.5 w-full"
          defaultValue={value ?? ""}
          placeholder="/path/to/project"
          onBlur={(e) => {
            const val = e.target.value.trim() || null;
            if (val !== (value ?? null)) onCommit(val);
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter") e.currentTarget.blur();
            if (e.key === "Escape") {
              e.currentTarget.value = value ?? "";
              e.currentTarget.blur();
            }
          }}
        />
      </div>
      <button
        type="button"
        className="shrink-0 rounded p-0.5 text-muted-foreground hover:text-foreground hover:bg-accent transition-colors"
        title="Browse directory"
        onClick={handleBrowse}
      >
        <FolderOpen className="h-3.5 w-3.5" />
      </button>
    </div>
  );
}
