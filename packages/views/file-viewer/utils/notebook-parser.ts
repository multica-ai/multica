/**
 * Jupyter Notebook (.ipynb) parser.
 *
 * Parses the JSON structure of a Jupyter notebook into a flat list of cells
 * with normalized content suitable for rendering.
 */

export interface NotebookCell {
  cellType: "code" | "markdown" | "raw";
  source: string;
  outputs: CellOutput[];
  executionCount: number | null;
  metadata: Record<string, unknown>;
}

export interface CellOutput {
  outputType: "stream" | "execute_result" | "display_data" | "error" | "unknown";
  text?: string;
  html?: string;
  imageData?: string; // base64 PNG/JPEG
  imageMimeType?: string;
  traceback?: string[];
  ename?: string;
  evalue?: string;
}

export interface ParsedNotebook {
  metadata: {
    kernelName: string;
    language: string;
    nbformatMajor: number;
  };
  cells: NotebookCell[];
}

function joinSource(source: string | string[]): string {
  if (Array.isArray(source)) return source.join("");
  return source;
}

function parseOutput(raw: Record<string, unknown>): CellOutput {
  const outputType = raw.output_type as string;

  if (outputType === "stream") {
    return {
      outputType: "stream",
      text: joinSource(raw.text as string | string[]),
    };
  }

  if (outputType === "execute_result" || outputType === "display_data") {
    const data = raw.data as Record<string, unknown> | undefined;
    const result: CellOutput = { outputType };

    if (data) {
      // Prefer HTML > image > text
      if (data["text/html"]) {
        result.html = joinSource(data["text/html"] as string | string[]);
      }
      if (data["image/png"]) {
        result.imageData = joinSource(data["image/png"] as string | string[]).replace(/\s/g, "");
        result.imageMimeType = "image/png";
      } else if (data["image/jpeg"]) {
        result.imageData = joinSource(data["image/jpeg"] as string | string[]).replace(/\s/g, "");
        result.imageMimeType = "image/jpeg";
      }
      if (data["text/plain"]) {
        result.text = joinSource(data["text/plain"] as string | string[]);
      }
    }

    return result;
  }

  if (outputType === "error") {
    return {
      outputType: "error",
      ename: raw.ename as string,
      evalue: raw.evalue as string,
      traceback: raw.traceback as string[],
    };
  }

  return { outputType: "unknown", text: JSON.stringify(raw, null, 2) };
}

export function parseNotebook(content: string): ParsedNotebook {
  const nb = JSON.parse(content) as Record<string, unknown>;

  const kernelSpec = (nb.metadata as Record<string, unknown>)?.kernelspec as Record<string, unknown> | undefined;
  const languageInfo = (nb.metadata as Record<string, unknown>)?.language_info as Record<string, unknown> | undefined;

  const metadata = {
    kernelName: (kernelSpec?.display_name as string) ?? (kernelSpec?.name as string) ?? "Unknown",
    language: (languageInfo?.name as string) ?? "python",
    nbformatMajor: (nb.nbformat as number) ?? 4,
  };

  const rawCells = (nb.cells as Array<Record<string, unknown>>) ?? [];

  const cells: NotebookCell[] = rawCells.map((cell) => ({
    cellType: cell.cell_type as "code" | "markdown" | "raw",
    source: joinSource(cell.source as string | string[]),
    outputs: ((cell.outputs as Array<Record<string, unknown>>) ?? []).map(parseOutput),
    executionCount: (cell.execution_count as number | null) ?? null,
    metadata: (cell.metadata as Record<string, unknown>) ?? {},
  }));

  return { metadata, cells };
}
