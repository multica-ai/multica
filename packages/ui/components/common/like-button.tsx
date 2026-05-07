"use client";

interface LikeButtonProps {
  onToggle: (emoji: string) => void;
  className?: string;
}

function LikeButton({ onToggle, className }: LikeButtonProps) {
  return (
    <button
      type="button"
      aria-label="Like"
      onClick={() => onToggle("👍")}
      className={`inline-flex items-center justify-center h-6 w-6 rounded-full text-muted-foreground hover:bg-accent hover:text-foreground transition-colors ${className ?? ""}`}
    >
      <span className="text-base leading-none">👍</span>
    </button>
  );
}

export { LikeButton };
