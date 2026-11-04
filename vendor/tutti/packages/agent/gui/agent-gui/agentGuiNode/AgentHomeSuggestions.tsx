import { memo, useState, type ComponentType, type SVGProps } from "react";
import {
  ArrowLeftRight,
  Code,
  Compass,
  Import,
  Pencil,
  X
} from "lucide-react";
import { GitHubBrandIcon } from "@tutti-os/ui-system/icons";
import styles from "./AgentGUINode.styles";
import type {
  AgentHomeSuggestionAction,
  AgentHomeSuggestionCategory,
  AgentHomeSuggestionIcon,
  AgentHomeSuggestionItem
} from "./model/agentGuiNodeTypes";

/** Tutti brand glyph for the "Meet Tutti" chip. */
function TuttiIcon(props: SVGProps<SVGSVGElement>): React.JSX.Element {
  return (
    <svg
      width="24"
      height="24"
      viewBox="0 0 24 24"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      {...props}
    >
      <path
        d="M11.9239 0.561676C15.4275 0.704643 18.0594 1.93856 19.9297 3.83316L20.0235 3.92984C23.0545 7.07999 23.0228 10.8742 22.9942 11.9015C22.8949 15.4888 21.1541 19.3943 18.0293 21.4328C15.586 23.0271 12.2829 23.8614 9.04007 23.215C5.73644 22.5564 2.62421 20.3849 0.626007 16.2238L3.20804 14.9836C4.83809 18.3782 7.22804 19.9325 9.60062 20.4054C12.0339 20.8904 14.5821 20.262 16.4649 19.0334C16.5072 19.0058 16.5482 18.9761 16.5899 18.9474C16.1093 18.8715 15.6404 18.7558 15.2119 18.5773C14.5812 18.3145 13.9737 17.8946 13.5371 17.2316C13.3482 16.9446 13.2091 16.6341 13.1094 16.3068V19.089C11.9062 19.089 10.438 19.0648 9.26761 18.5773C8.63673 18.3144 8.0285 17.8949 7.59183 17.2316C7.19348 16.6265 7.00806 15.9216 6.97952 15.1593C5.38231 15.2733 4.05048 14.8926 3.01273 14.0617C1.45415 12.8137 0.967716 10.8679 0.965851 9.3605C0.960457 5.06718 4.93349 0.556422 11.211 0.548004L11.9239 0.561676ZM12.3096 13.4562C11.6218 13.7868 10.8654 14.1393 9.99808 14.4611C9.94487 14.4808 9.89073 14.4979 9.83792 14.5168V14.9503C9.83792 15.3764 9.92819 15.571 9.98441 15.6564C10.0384 15.7384 10.1371 15.8371 10.3692 15.9337C10.921 16.1636 11.797 16.223 13.084 16.2238C12.9714 15.8224 12.918 15.3955 12.918 14.9503V13.1632C12.7221 13.2577 12.5204 13.3549 12.3096 13.4562ZM11.2149 3.41226C6.37997 3.4188 3.86603 6.76409 3.83011 9.29703V9.3566C3.83137 10.3769 4.17272 11.3206 4.80276 11.8253C5.17526 12.1236 5.83321 12.4102 6.97366 12.2882V6.38785H9.83792V11.4386C10.2583 11.2583 10.6625 11.0703 11.0684 10.8752C11.6533 10.594 12.2733 10.2876 12.918 10.008V6.38785H15.7823V9.08414C16.6423 8.90981 17.6005 8.80628 18.708 8.80484L18.7119 11.6691C17.5478 11.6706 16.6096 11.8073 15.7823 12.0255V14.9503C15.7823 15.3762 15.8725 15.5709 15.9287 15.6564C15.9827 15.7384 16.0816 15.8371 16.3135 15.9337C16.8534 16.1586 17.7037 16.2204 18.9453 16.2238C19.6618 14.896 20.0887 13.3489 20.1309 11.8214C20.1545 10.9724 20.155 8.19654 17.96 5.91519C16.5592 4.45958 14.4443 3.40841 11.2149 3.41226Z"
        fill="currentColor"
      />
    </svg>
  );
}

/** Checklist glyph for the "Task breakdown" chip. */
function TaskBreakdownIcon(props: SVGProps<SVGSVGElement>): React.JSX.Element {
  return (
    <svg
      width="24"
      height="24"
      viewBox="0 0 24 24"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      {...props}
    >
      <path
        d="M8.29297 14.793C8.68349 14.4025 9.31651 14.4025 9.70703 14.793C10.0976 15.1835 10.0976 15.8166 9.70703 16.2071L5.70703 20.2071C5.31651 20.5976 4.68349 20.5976 4.29297 20.2071L2.29297 18.2071C1.90244 17.8166 1.90244 17.1836 2.29297 16.793C2.68349 16.4025 3.31651 16.4025 3.70703 16.793L5 18.086L8.29297 14.793ZM21 18.0001C21.5523 18.0001 22 18.4478 22 19.0001C22 19.5524 21.5523 20.0001 21 20.0001H13C12.4477 20.0001 12 19.5524 12 19.0001C12 18.4478 12.4477 18.0001 13 18.0001H21ZM21 11C21.5523 11 22 11.4478 22 12C22 12.5523 21.5523 13 21 13H13C12.4477 13 12 12.5523 12 12C12 11.4478 12.4477 11 13 11H21ZM8 3.5C9.10457 3.5 10 4.39543 10 5.50001V9.50003C10 10.6046 9.10457 11.5 8 11.5H4C2.89543 11.5 2 10.6046 2 9.50003V5.50001C2 4.39543 2.89543 3.5 4 3.5H8ZM4 9.50003H8V5.50001H4V9.50003ZM21 4C21.5523 4 22 4.44772 22 5.00001C22 5.55229 21.5523 6.00001 21 6.00001H13C12.4477 6.00001 12 5.55229 12 5.00001C12 4.44772 12.4477 4 13 4H21Z"
        fill="currentColor"
      />
    </svg>
  );
}

/** Crossed-swords glyph for the "Agent interaction" chip. */
function BattleIcon(props: SVGProps<SVGSVGElement>): React.JSX.Element {
  return (
    <svg
      width="24"
      height="24"
      viewBox="0 0 24 24"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      {...props}
    >
      <path
        d="M4.29297 13.293C4.68349 12.9024 5.31651 12.9024 5.70703 13.293L9.70703 17.293C10.0976 17.6835 10.0976 18.3165 9.70703 18.707C9.31651 19.0976 8.68349 19.0976 8.29297 18.707L7.5 17.9141L5.41406 20L5.70703 20.293C6.09756 20.6835 6.09756 21.3165 5.70703 21.707C5.31651 22.0976 4.68349 22.0976 4.29297 21.707L2.29297 19.707C1.90244 19.3165 1.90244 18.6835 2.29297 18.293C2.68349 17.9024 3.31651 17.9024 3.70703 18.293L4 18.5859L6.08594 16.5L4.29297 14.707C3.90244 14.3165 3.90244 13.6835 4.29297 13.293ZM6 2C6.26522 2 6.51949 2.10543 6.70703 2.29297L17.5 13.0859L18.293 12.293C18.6835 11.9024 19.3165 11.9024 19.707 12.293C20.0976 12.6835 20.0976 13.3165 19.707 13.707L17.4141 16L20 18.5859L20.293 18.293C20.6835 17.9024 21.3165 17.9024 21.707 18.293C22.0976 18.6835 22.0976 19.3165 21.707 19.707L19.707 21.707C19.3165 22.0976 18.6835 22.0976 18.293 21.707C17.9024 21.3165 17.9024 20.6835 18.293 20.293L18.5859 20L16 17.4141L13.707 19.707C13.3165 20.0976 12.6835 20.0976 12.293 19.707C11.9024 19.3165 11.9024 18.6835 12.293 18.293L13.0859 17.5L2.29297 6.70703C2.10543 6.51949 2 6.26522 2 6V3C2 2.44772 2.44772 2 3 2H6ZM4 5.58594L14.5 16.0859L16.0859 14.5L5.58594 4H4V5.58594ZM21 2C21.5523 2 22 2.44772 22 3V6C22 6.26522 21.8946 6.51949 21.707 6.70703L18.207 10.207C17.8165 10.5976 17.1835 10.5976 16.793 10.207C16.4024 9.81651 16.4024 9.18349 16.793 8.79297L20 5.58594V4H18.4141L15.207 7.20703C14.8165 7.59756 14.1835 7.59756 13.793 7.20703C13.4024 6.81651 13.4024 6.18349 13.793 5.79297L17.293 2.29297L17.3662 2.22656C17.5442 2.08073 17.7679 2 18 2H21Z"
        fill="currentColor"
      />
    </svg>
  );
}

/** Speech-bubble with code brackets for the "Quality review" chip. */
function QualityReviewIcon(props: SVGProps<SVGSVGElement>): React.JSX.Element {
  return (
    <svg
      width="24"
      height="24"
      viewBox="0 0 24 24"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      {...props}
    >
      <path
        d="M20 2C20.7957 2 21.5585 2.3163 22.1211 2.87891C22.6837 3.44152 23 4.20435 23 5V17C23 17.7957 22.6837 18.5585 22.1211 19.1211C21.5585 19.6837 20.7957 20 20 20H6.82812C6.56296 20.0001 6.30857 20.1054 6.12109 20.293L3.91895 22.4951C3.6799 22.7341 3.37545 22.8969 3.04395 22.9629C2.71233 23.0288 2.36805 22.9956 2.05566 22.8662C1.74327 22.7368 1.47598 22.5174 1.28809 22.2363C1.10019 21.9552 1.00004 21.6243 1 21.2861V5C1 4.20435 1.3163 3.44152 1.87891 2.87891C2.44152 2.3163 3.20435 2 4 2H20ZM4 4C3.73478 4 3.4805 4.10543 3.29297 4.29297C3.10543 4.4805 3 4.73478 3 5V20.5859L4.70703 18.8789L4.92676 18.6797C5.46048 18.2422 6.13198 18.0001 6.82812 18H20C20.2652 18 20.5195 17.8946 20.707 17.707C20.8946 17.5195 21 17.2652 21 17V5C21 4.73478 20.8946 4.48051 20.707 4.29297C20.5195 4.10543 20.2652 4 20 4H4ZM9.29297 7.29297C9.68349 6.90244 10.3165 6.90244 10.707 7.29297C11.0976 7.68349 11.0976 8.31651 10.707 8.70703L8.41406 11L10.707 13.293C11.0976 13.6835 11.0976 14.3165 10.707 14.707C10.3165 15.0976 9.68349 15.0976 9.29297 14.707L6.29297 11.707C5.90244 11.3165 5.90244 10.6835 6.29297 10.293L9.29297 7.29297ZM13.293 7.29297C13.6835 6.90244 14.3165 6.90244 14.707 7.29297L17.707 10.293C18.0976 10.6835 18.0976 11.3165 17.707 11.707L14.707 14.707C14.3165 15.0976 13.6835 15.0976 13.293 14.707C12.9024 14.3165 12.9024 13.6835 13.293 13.293L15.5859 11L13.293 8.70703C12.9024 8.31651 12.9024 7.68349 13.293 7.29297Z"
        fill="currentColor"
      />
    </svg>
  );
}

const CATEGORY_ICON = {
  write: Pencil,
  code: Code,
  research: Compass,
  handoff: ArrowLeftRight,
  breakdown: TaskBreakdownIcon,
  review: QualityReviewIcon,
  interaction: BattleIcon,
  github: GitHubBrandIcon,
  about: TuttiIcon,
  import: Import
} as const satisfies Record<
  AgentHomeSuggestionIcon,
  ComponentType<SVGProps<SVGSVGElement>>
>;

function suggestionPrompt(item: AgentHomeSuggestionItem): string {
  return item.prompt ?? item.label;
}

/**
 * A chip that acts immediately on click (fills the composer or triggers a host
 * action) rather than expanding a card of `items`.
 */
function isDirectCategory(category: AgentHomeSuggestionCategory): boolean {
  const hasItems = category.items !== undefined && category.items.length > 0;
  if (hasItems) {
    return false;
  }
  return (
    (category.prompt !== undefined && category.prompt.length > 0) ||
    category.action !== undefined
  );
}

export interface AgentHomeSuggestionsProps {
  categories: readonly AgentHomeSuggestionCategory[];
  /** Prefill the composer with the chosen suggestion's prompt. */
  onSelectSuggestion: (prompt: string) => void;
  /** Trigger a host action for a chip that carries one (e.g. import session). */
  onSelectAction?: (action: AgentHomeSuggestionAction) => void;
  /** Accessible label for the close button on the expanded category card. */
  closeLabel?: string;
}

/**
 * Starter-prompt suggestions rendered under the new-session composer on the
 * empty hero. A row of category chips; selecting one expands that category's
 * suggestions in a card. Clicking a suggestion prefills the composer (it does
 * not auto-send) so the user can edit before sending.
 */
export const AgentHomeSuggestions = memo(function AgentHomeSuggestions({
  categories,
  onSelectSuggestion,
  onSelectAction,
  closeLabel
}: AgentHomeSuggestionsProps): React.JSX.Element | null {
  "use memo";

  const [activeCategoryId, setActiveCategoryId] = useState<string | null>(null);

  if (categories.length === 0) {
    return null;
  }

  const activeCategory =
    categories.find(
      (category) =>
        category.id === activeCategoryId && !isDirectCategory(category)
    ) ?? null;
  const ActiveCategoryIcon = activeCategory?.icon
    ? CATEGORY_ICON[activeCategory.icon]
    : null;

  return (
    <div className={styles.emptyHeroSuggestions}>
      <div className={styles.emptyHeroSuggestionsChips}>
        {categories.map((category) => {
          const Icon = category.icon ? CATEGORY_ICON[category.icon] : null;
          const direct = isDirectCategory(category);
          const isActive = !direct && category.id === activeCategory?.id;
          return (
            <button
              key={category.id}
              type="button"
              className={styles.emptyHeroSuggestionsChip}
              aria-pressed={isActive}
              onClick={() => {
                if (direct) {
                  // A chip may carry both: fill the prompt first, then run the
                  // action (e.g. open the @ palette on a specific tab).
                  if (category.prompt) {
                    onSelectSuggestion(category.prompt);
                  }
                  if (category.action) {
                    onSelectAction?.(category.action);
                  }
                  return;
                }
                setActiveCategoryId((current) =>
                  current === category.id ? null : category.id
                );
              }}
            >
              {Icon ? (
                <Icon
                  className={styles.emptyHeroSuggestionsChipIcon}
                  aria-hidden
                />
              ) : null}
              <span>{category.label}</span>
            </button>
          );
        })}
      </div>
      {activeCategory ? (
        <div className={styles.emptyHeroSuggestionsCard}>
          <div className={styles.emptyHeroSuggestionsCardHeader}>
            <span className={styles.emptyHeroSuggestionsCardTitle}>
              {ActiveCategoryIcon ? (
                <ActiveCategoryIcon
                  className={styles.emptyHeroSuggestionsChipIcon}
                  aria-hidden
                />
              ) : null}
              {activeCategory.label}
            </span>
            <button
              type="button"
              className={styles.emptyHeroSuggestionsCardClose}
              aria-label={closeLabel}
              title={closeLabel}
              onClick={() => setActiveCategoryId(null)}
            >
              <X aria-hidden />
            </button>
          </div>
          {activeCategory.items?.map((item) => (
            <button
              key={item.id}
              type="button"
              className={styles.emptyHeroSuggestionsItem}
              onClick={() => onSelectSuggestion(suggestionPrompt(item))}
            >
              {item.label}
            </button>
          ))}
        </div>
      ) : null}
    </div>
  );
});
