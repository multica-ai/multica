import { cn } from "@multica/ui/lib/utils"
import { Loader2Icon } from "lucide-react"
import { useTranslation } from "react-i18next"

function Spinner({ className, "aria-label": ariaLabel, ...props }: React.ComponentProps<"svg">) {
  const { t } = useTranslation("ui")
  return (
    <Loader2Icon
      role="status"
      aria-label={ariaLabel ?? t(($) => $.loading)}
      className={cn("size-4 animate-spin", className)}
      {...props}
    />
  )
}

export { Spinner }
