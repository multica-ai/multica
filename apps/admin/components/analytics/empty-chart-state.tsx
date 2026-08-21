import { BarChart3 } from "lucide-react";

export function EmptyChartState({ message }: { message: string }) {
  return (
    <div className="flex aspect-[3/1] flex-col items-center justify-center gap-2 rounded-md border border-dashed bg-muted/20 p-6 text-center">
      <BarChart3 className="h-5 w-5 text-muted-foreground" />
      <p className="text-caption text-muted-foreground">{message}</p>
    </div>
  );
}
