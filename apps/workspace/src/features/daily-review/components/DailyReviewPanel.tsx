import { useEffect, useState } from "react";
import { CheckCircle, RefreshCw, BookOpen } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { toast } from "sonner";
import {
  useTodayReviewQuery,
  useGenerateReviewMutation,
  useConfirmReviewMutation,
} from "../hooks/use-daily-review";
import { ReviewMarkdownView } from "./ReviewMarkdownView";

/**
 * Panel displayed on MyTimePage showing today's nightly review draft.
 * Users can generate (or regenerate) the draft and confirm it when done.
 */
export function DailyReviewPanel() {
  const { data: review, isLoading } = useTodayReviewQuery();
  const generate = useGenerateReviewMutation();
  const confirm = useConfirmReviewMutation();
  const [energyLevel, setEnergyLevel] = useState<number | "">("");
  const [energyNote, setEnergyNote] = useState("");
  const [recoveryNeed, setRecoveryNeed] = useState(false);

  useEffect(() => {
    if (!review) return;
    setEnergyLevel(review.energy_level ?? "");
    setEnergyNote(review.energy_note ?? "");
    setRecoveryNeed(review.recovery_need ?? false);
  }, [review?.id, review?.energy_level, review?.energy_note, review?.recovery_need]);

  const handleGenerate = () => {
    generate.mutate(undefined, {
      onSuccess: () => toast.success("今日复盘已生成"),
      onError: () => toast.error("复盘生成失败，请重试"),
    });
  };

  const handleConfirm = () => {
    if (!review) return;
    confirm.mutate({
      reviewId: review.id,
      data: {
        energy_level: energyLevel === "" ? undefined : energyLevel,
        energy_note: energyNote.trim() || undefined,
        recovery_need: recoveryNeed,
      },
    }, {
      onSuccess: () => toast.success("复盘已确认"),
      onError: () => toast.error("确认失败，请重试"),
    });
  };

  return (
    <div className="rounded-lg border bg-card p-4 space-y-3">
      {/* Header row */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2 text-sm font-medium">
          <BookOpen className="h-4 w-4 text-muted-foreground" />
          <span>今日复盘</span>
          {review?.status === "confirmed" && (
            <CheckCircle className="h-4 w-4 text-green-500" />
          )}
        </div>
        <div className="flex items-center gap-2">
          {review?.status !== "confirmed" && review && (
            <Button
              size="sm"
              variant="outline"
              onClick={handleConfirm}
              disabled={confirm.isPending}
            >
              <CheckCircle className="mr-1 h-3 w-3" />
              确认
            </Button>
          )}
          <Button
            size="sm"
            variant="outline"
            onClick={handleGenerate}
            disabled={generate.isPending}
          >
            <RefreshCw className={`mr-1 h-3 w-3 ${generate.isPending ? "animate-spin" : ""}`} />
            {review ? "重新生成" : "生成复盘"}
          </Button>
        </div>
      </div>

      {/* Content area */}
      {isLoading ? (
        <div className="space-y-2">
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-3/4" />
          <Skeleton className="h-4 w-5/6" />
        </div>
      ) : review ? (
        <div className="space-y-3">
          <ReviewMarkdownView review={review} />
          {review.status !== "confirmed" ? (
            <div className="space-y-3 rounded-md border bg-muted/20 p-3">
              <div className="grid gap-1.5">
                <Label htmlFor="energy-level" className="text-xs">今日精力</Label>
                <select
                  id="energy-level"
                  value={energyLevel}
                  onChange={(event) => setEnergyLevel(event.target.value === "" ? "" : Number(event.target.value))}
                  className="h-8 rounded-md border bg-background px-2 text-sm"
                >
                  <option value="">不记录</option>
                  <option value={1}>1 - 很低</option>
                  <option value={2}>2 - 偏低</option>
                  <option value={3}>3 - 正常</option>
                  <option value={4}>4 - 良好</option>
                  <option value={5}>5 - 很好</option>
                </select>
              </div>
              <div className="flex items-center gap-2">
                <Checkbox
                  id="recovery-need"
                  checked={recoveryNeed}
                  onCheckedChange={(checked) => setRecoveryNeed(checked === true)}
                />
                <Label htmlFor="recovery-need" className="text-xs text-muted-foreground">
                  明天需要降低负载或安排恢复
                </Label>
              </div>
              <Input
                value={energyNote}
                onChange={(event) => setEnergyNote(event.target.value)}
                placeholder="精力备注，可选"
                className="h-8 text-sm"
              />
            </div>
          ) : null}
        </div>
      ) : (
        <p className="text-sm text-muted-foreground">
          今日复盘尚未生成。点击「生成复盘」查看 AI 为你准备的回顾。
        </p>
      )}
    </div>
  );
}
