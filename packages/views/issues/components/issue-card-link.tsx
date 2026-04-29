"use client";

import { forwardRef } from "react";
import { useNavigation } from "../../navigation";

interface IssueCardLinkProps
  extends React.AnchorHTMLAttributes<HTMLAnchorElement> {
  href: string;
  onOpenIssue?: () => void;
}

export const IssueCardLink = forwardRef<HTMLAnchorElement, IssueCardLinkProps>(
  function IssueCardLink(
    { href, children, onClick, onOpenIssue, ...props },
    ref,
  ) {
    const navigation = useNavigation();

    const handleClick = (e: React.MouseEvent<HTMLAnchorElement>) => {
      onClick?.(e);
      if (e.defaultPrevented) return;

      if (e.metaKey || e.ctrlKey || e.shiftKey) {
        if (navigation.openInNewTab) {
          e.preventDefault();
          navigation.openInNewTab(href);
        }
        return;
      }

      e.preventDefault();
      if (onOpenIssue) {
        onOpenIssue();
        return;
      }
      navigation.push(href);
    };

    return (
      <a ref={ref} href={href} onClick={handleClick} {...props}>
        {children}
      </a>
    );
  },
);
