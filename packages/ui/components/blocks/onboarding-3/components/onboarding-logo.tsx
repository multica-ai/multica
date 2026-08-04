import { cn } from "@multica/ui/lib/utils"
import { Item, ItemMedia } from "@multica/ui/components/ui/item"

export function OnboardingLogo({ className }: { className?: string }) {
  return (
    <div className={cn("inline-flex items-center gap-2", className)}>
      <Item
        className="bg-primary text-primary-foreground flex size-7 shrink-0 items-center justify-center p-1.5"
        aria-hidden="true"
      >
        <ItemMedia variant="icon" className="size-auto">
          <svg
            width="50"
            height="50"
            viewBox="25.668 25.1352 49.6644 50"
            fill="none"
            xmlns="http://www.w3.org/2000/svg"
            className="size-[0.95rem]"
          >
            <circle cx="70.634" cy="29.8334" r="4.69799" fill="currentColor" />
            <path
              fillRule="evenodd"
              clipRule="evenodd"
              d="M25.668 57.0144V29.8332C25.668 27.2386 27.7713 25.1352 30.366 25.1352C32.9606 25.1352 35.0639 27.2386 35.0639 29.8332V57.0144C35.0639 61.833 38.9702 65.7392 43.7888 65.7392H57.2116C62.0302 65.7392 65.9364 61.833 65.9364 57.0144V43.7258C65.9364 41.1312 68.0398 39.0278 70.6344 39.0278C73.229 39.0278 75.3324 41.1312 75.3324 43.7258V57.0144C75.3324 67.0222 67.2194 75.1352 57.2116 75.1352H43.7888C33.7809 75.1352 25.668 67.0222 25.668 57.0144Z"
              fill="currentColor"
            />
          </svg>
        </ItemMedia>
      </Item>

      <span className="text-body-lg font-medium">ReUI</span>
    </div>
  )
}