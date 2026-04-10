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
 * A text input for filesystem paths with an optional folder-picker button.
 * In Electron the folder picker resolves the full absolute path via `file.path`.
 * In a browser context the user can still type the path manually.
 */
export function DirInput({ value, inputKey, className, onCommit }: DirInputProps) {
  const inputRef = useRef<HTMLInputElement>(null);
  const pickerRef = useRef<HTMLInputElement>(null);

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
        onClick={() => pickerRef.current?.click()}
      >
        <FolderOpen className="h-3.5 w-3.5" />
      </button>
      <input
        ref={pickerRef}
        type="file"
        className="hidden"
        {...{ webkitdirectory: "" } as React.InputHTMLAttributes<HTMLInputElement>}
        onChange={(e) => {
          const file = e.target.files?.[0];
          if (!file) return;
          const filePath = (file as File & { path?: string }).path;
          const dir = filePath
            ? filePath.substring(0, Math.max(filePath.lastIndexOf("/"), filePath.lastIndexOf("\\")))
            : (file.webkitRelativePath.split("/")[0] ?? "");
          if (inputRef.current) inputRef.current.value = dir;
          onCommit(dir || null);
          e.target.value = "";
        }}
      />
    </div>
  );
}
