"use client";

import { useState, useCallback, useRef } from "react";
import {
  Upload,
  FileText,
  FileCode2,
  FileSpreadsheet,
  FileImage,
  File,
} from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { FileViewer } from "./file-viewer";
import { detectFileCategory } from "./utils/file-type";

// ---------------------------------------------------------------------------
// Sample files for demo / empty state
// ---------------------------------------------------------------------------

const SAMPLE_FILES: Array<{ name: string; content: string }> = [
  {
    name: "example.md",
    content: `# Welcome to the File Viewer

This viewer supports **multiple file formats**:

- Markdown (.md, .mdx)
- Jupyter Notebooks (.ipynb)
- Code files (50+ languages with syntax highlighting)
- PDF documents
- Images (PNG, JPG, SVG, etc.)

## Features

| Feature | Status |
|---------|--------|
| Syntax highlighting | Shiki-powered |
| Line numbers | Code viewer |
| Frontmatter parsing | Markdown |
| Cell rendering | Notebooks |
| Zoom controls | Images |

> Drag & drop a file or click "Open File" to get started.
`,
  },
  {
    name: "example.py",
    content: `"""Example Python file for the code viewer."""

import numpy as np
from typing import TypeVar, Generic

T = TypeVar("T")


class DataPipeline(Generic[T]):
    """A simple data processing pipeline."""

    def __init__(self, name: str) -> None:
        self.name = name
        self._steps: list[callable] = []

    def add_step(self, fn: callable) -> "DataPipeline[T]":
        self._steps.append(fn)
        return self

    def run(self, data: T) -> T:
        result = data
        for step in self._steps:
            result = step(result)
        return result


def main():
    pipeline = DataPipeline[np.ndarray]("transform")
    pipeline.add_step(lambda x: x * 2)
    pipeline.add_step(lambda x: x + 1)

    data = np.array([1, 2, 3, 4, 5])
    result = pipeline.run(data)
    print(f"Result: {result}")


if __name__ == "__main__":
    main()
`,
  },
  {
    name: "example.ipynb",
    content: JSON.stringify(
      {
        metadata: {
          kernelspec: {
            display_name: "Python 3",
            language: "python",
            name: "python3",
          },
          language_info: { name: "python", version: "3.11.0" },
        },
        nbformat: 4,
        nbformat_minor: 5,
        cells: [
          {
            cell_type: "markdown",
            metadata: {},
            source: ["# Data Analysis Notebook\n", "\n", "A demo notebook showing the notebook viewer."],
          },
          {
            cell_type: "code",
            execution_count: 1,
            metadata: {},
            source: ["import numpy as np\nimport pandas as pd\n\n# Create sample data\ndata = pd.DataFrame({\n    'x': np.random.randn(100),\n    'y': np.random.randn(100),\n    'category': np.random.choice(['A', 'B', 'C'], 100)\n})\ndata.head()"],
            outputs: [
              {
                output_type: "execute_result",
                data: {
                  "text/plain": [
                    "          x         y category\n",
                    "0  0.496714 -0.234137        A\n",
                    "1 -0.138264  1.579213        B\n",
                    "2  0.647689 -0.519418        C\n",
                    "3  1.523030  0.767435        A\n",
                    "4 -0.234153  0.542560        B",
                  ],
                },
                execution_count: 1,
                metadata: {},
              },
            ],
          },
          {
            cell_type: "code",
            execution_count: 2,
            metadata: {},
            source: ["# Summary statistics\ndata.describe()"],
            outputs: [
              {
                output_type: "execute_result",
                data: {
                  "text/plain": [
                    "                x           y\n",
                    "count  100.000000  100.000000\n",
                    "mean    -0.010934    0.048042\n",
                    "std      1.013328    0.987654\n",
                    "min     -2.552990   -2.301539\n",
                    "max      2.269755    2.852656",
                  ],
                },
                execution_count: 2,
                metadata: {},
              },
            ],
          },
          {
            cell_type: "markdown",
            metadata: {},
            source: ["## Conclusion\n", "\n", "The data shows a **normal distribution** for both variables."],
          },
        ],
      },
      null,
      2,
    ),
  },
];

// ---------------------------------------------------------------------------
// File entry in the sidebar
// ---------------------------------------------------------------------------

interface LoadedFile {
  name: string;
  content: string;
  url?: string;
}

const CATEGORY_ICONS: Record<string, typeof File> = {
  notebook: FileSpreadsheet,
  markdown: FileText,
  code: FileCode2,
  image: FileImage,
  pdf: FileText,
  text: File,
};

function FileEntry({
  file,
  isActive,
  onClick,
}: {
  file: LoadedFile;
  isActive: boolean;
  onClick: () => void;
}) {
  const category = detectFileCategory(file.name);
  const Icon = CATEGORY_ICONS[category] ?? File;

  return (
    <button
      onClick={onClick}
      className={`flex items-center gap-2 w-full rounded-md px-3 py-2 text-left text-sm transition-colors ${
        isActive
          ? "bg-primary/10 text-primary font-medium"
          : "text-muted-foreground hover:bg-muted/50 hover:text-foreground"
      }`}
    >
      <Icon className="size-4 shrink-0" />
      <span className="truncate">{file.name}</span>
    </button>
  );
}

// ---------------------------------------------------------------------------
// Viewer page
// ---------------------------------------------------------------------------

export function ViewerPage() {
  const [files, setFiles] = useState<LoadedFile[]>(SAMPLE_FILES);
  const [activeIndex, setActiveIndex] = useState(0);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const activeFile = files[activeIndex];

  const handleFileUpload = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      const uploadedFiles = e.target.files;
      if (!uploadedFiles) return;

      Array.from(uploadedFiles).forEach((file) => {
        const category = detectFileCategory(file.name);

        if (category === "pdf" || category === "image") {
          // Binary files — create object URL
          const url = URL.createObjectURL(file);
          setFiles((prev) => [...prev, { name: file.name, content: "", url }]);
          setActiveIndex(files.length);
        } else {
          // Text files — read content
          const reader = new FileReader();
          reader.onload = (ev) => {
            const content = ev.target?.result as string;
            setFiles((prev) => [...prev, { name: file.name, content }]);
            setActiveIndex(files.length);
          };
          reader.readAsText(file);
        }
      });

      // Reset input so the same file can be selected again
      e.target.value = "";
    },
    [files.length],
  );

  const handleContentChange = useCallback(
    (content: string) => {
      setFiles((prev) =>
        prev.map((f, i) => (i === activeIndex ? { ...f, content } : f)),
      );
    },
    [activeIndex],
  );

  // Drag & drop handler
  const handleDrop = useCallback(
    (e: React.DragEvent) => {
      e.preventDefault();
      const droppedFiles = e.dataTransfer.files;
      if (!droppedFiles.length) return;

      Array.from(droppedFiles).forEach((file) => {
        const category = detectFileCategory(file.name);

        if (category === "pdf" || category === "image") {
          const url = URL.createObjectURL(file);
          setFiles((prev) => [...prev, { name: file.name, content: "", url }]);
        } else {
          const reader = new FileReader();
          reader.onload = (ev) => {
            const content = ev.target?.result as string;
            setFiles((prev) => [...prev, { name: file.name, content }]);
          };
          reader.readAsText(file);
        }
      });
      setActiveIndex(files.length);
    },
    [files.length],
  );

  return (
    <div className="flex h-full">
      {/* Sidebar */}
      <div className="w-56 shrink-0 border-r flex flex-col">
        <div className="flex items-center justify-between px-4 py-3 border-b">
          <h2 className="text-sm font-semibold">Files</h2>
          <Button
            variant="ghost"
            size="icon-xs"
            onClick={() => fileInputRef.current?.click()}
            className="text-muted-foreground"
          >
            <Upload className="size-3.5" />
          </Button>
          <input
            ref={fileInputRef}
            type="file"
            multiple
            className="hidden"
            onChange={handleFileUpload}
          />
        </div>
        <div className="flex-1 overflow-y-auto p-2 space-y-0.5">
          {files.map((file, i) => (
            <FileEntry
              key={`${file.name}-${i}`}
              file={file}
              isActive={i === activeIndex}
              onClick={() => setActiveIndex(i)}
            />
          ))}
        </div>
      </div>

      {/* Main viewer area */}
      <div
        className="flex-1 min-w-0 p-4"
        onDragOver={(e) => e.preventDefault()}
        onDrop={handleDrop}
      >
        {activeFile ? (
          <div className="h-full">
            <FileViewer
              path={activeFile.name}
              content={activeFile.content}
              url={activeFile.url}
              onChange={handleContentChange}
            />
          </div>
        ) : (
          <div className="flex items-center justify-center h-full text-muted-foreground">
            <div className="text-center space-y-3">
              <Upload className="size-12 mx-auto text-muted-foreground/30" />
              <p className="text-sm">Drop files here or click Upload</p>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
