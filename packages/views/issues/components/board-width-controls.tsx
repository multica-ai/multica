"use client";

import { useId } from "react";
import {
  BOARD_COLUMN_MIN_WIDTH,
  BOARD_COLUMN_MAX_WIDTH,
} from "@multica/core/issues/stores/view-store";
import { useViewStore } from "@multica/core/issues/stores/view-store-context";
import { Slider } from "@multica/ui/components/ui/slider";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";

export function BoardWidthControls() {
  const { t } = useT("issues");
  const width = useViewStore((s) => s.boardColumnWidth);
  const fit = useViewStore((s) => s.boardFitToScreen);
  const setWidth = useViewStore((s) => s.setBoardColumnWidth);
  const setFit = useViewStore((s) => s.setBoardFitToScreen);
  const labelId = useId();

  return (
    <div className="flex items-center gap-2">
      <span id={labelId} className="shrink-0 text-caption font-medium text-muted-foreground">
        {t(($) => $.display.column_width)}
      </span>
      <Slider
        className="min-w-0 flex-1"
        aria-labelledby={labelId}
        min={BOARD_COLUMN_MIN_WIDTH}
        max={BOARD_COLUMN_MAX_WIDTH}
        step={10}
        value={[width]}
        disabled={fit}
        onValueChange={(value) => setWidth(Array.isArray(value) ? value[0]! : value)}
      />
      <span className="shrink-0 text-caption tabular-nums text-muted-foreground">
        {t(($) => $.display.column_width_value, { width })}
      </span>
      <Button
        type="button"
        size="sm"
        variant={fit ? "brand" : "outline"}
        aria-pressed={fit}
        onClick={() => setFit(!fit)}
      >
        {t(($) => $.display.fit_to_screen)}
      </Button>
    </div>
  );
}
