import { useMemo } from "react";
import type { ReactNode } from "react";
import { Spinner } from "@tutti-os/ui-system";
import { observer } from "mobx-react-lite";
export const AgentGUIStartupShell = observer(AgentGUIStartupShellRender);

const conversationSkeletonRowSizes = [
  "long",
  "medium",
  "short",
  "long",
  "medium"
] as const;

export interface AgentGUIStartupShellProps {
  loadingLabel: string;
}

function AgentGUIStartupShellRender({
  loadingLabel
}: AgentGUIStartupShellProps): ReactNode {
  const styleProp1 = useMemo<NonNullable<React.ComponentProps<"div">["style"]>>(
    () => ({ gridTemplateColumns: "52px 280px minmax(0, 1fr)" }),
    []
  );
  return (
    <div
      aria-busy="true"
      className="agent-gui-node__shell h-full min-h-0 min-w-0"
      data-agent-gui-startup-shell="body"
    >
      <div className="agent-gui-node__body">
        <div className="agent-gui-node__layout" style={styleProp1}>
          <aside className="flex h-full min-h-0 flex-col items-center gap-3 overflow-hidden bg-[var(--background-session-sidepanel)] px-2 pt-12">
            {[0, 1, 2].map((index) => (
              <span
                aria-hidden="true"
                className="size-8 shrink-0 rounded-lg bg-[var(--transparency-block)]"
                key={index}
              />
            ))}
          </aside>
          <aside className="flex h-full min-h-0 min-w-0 flex-col overflow-hidden bg-[var(--background-session-sidepanel)] pt-12">
            <div className="flex shrink-0 items-center gap-2 px-4 pb-4">
              <span
                aria-hidden="true"
                className="h-8 min-w-0 flex-1 rounded-md bg-[var(--transparency-block)]"
              />
              <span
                aria-hidden="true"
                className="size-8 shrink-0 rounded-md bg-[var(--transparency-block)]"
              />
            </div>
            <div className="min-h-0 flex-1 overflow-hidden px-4">
              <div className="agent-gui-node__conversation-list-skeleton">
                {conversationSkeletonRowSizes.map((size, index) => (
                  <div
                    className="agent-gui-node__conversation-list-skeleton-row"
                    data-size={size}
                    key={`${size}-${index}`}
                  >
                    <span className="agent-gui-node__conversation-list-skeleton-spine" />
                    <span className="agent-gui-node__conversation-list-skeleton-rib agent-gui-node__conversation-list-skeleton-rib-primary" />
                    <span className="agent-gui-node__conversation-list-skeleton-rib agent-gui-node__conversation-list-skeleton-rib-secondary" />
                  </div>
                ))}
              </div>
            </div>
          </aside>
          <section className="agent-gui-node__detail-panel">
            <div className="agent-gui-node__detail pt-11">
              <div className="flex h-full min-h-0 flex-1 flex-col">
                <div className="agent-gui-node__timeline agent-gui-node__timeline-centered">
                  <div
                    className="grid min-w-full grid-cols-[minmax(0,1fr)] gap-6"
                    data-agent-gui-startup-timeline-content="true"
                  >
                    <div className="agent-gui-node__empty-hero">
                      <div className="agent-gui-node__empty-hero-body">
                        <div
                          className="agent-gui-node__empty-hero-icon-slot"
                          data-carousel-placeholder={true}
                        >
                          <div
                            aria-hidden="true"
                            className="flex h-28 w-full items-center justify-center gap-4"
                          >
                            <span className="h-16 w-14 -rotate-6 rounded-lg bg-[var(--transparency-block)]" />
                            <span className="size-20 rounded-xl bg-[var(--transparency-block)]" />
                            <span className="h-16 w-14 rotate-6 rounded-lg bg-[var(--transparency-block)]" />
                          </div>
                        </div>
                        <h2 className="agent-gui-node__empty-hero-title">
                          <span
                            aria-hidden="true"
                            className="inline-block h-9 w-[min(440px,80%)] rounded-md bg-[var(--transparency-block)]"
                          />
                        </h2>
                        <div
                          className="agent-gui-node__composer-hero"
                          data-layout="hero"
                        >
                          <div className="agent-gui-node__composer-input-group agent-gui-node__composer-input-group-hero">
                            <div className="agent-gui-node__composer-input-shell agent-gui-node__composer-input-shell-hero">
                              <div className="agent-gui-node__composer-hero-prompt-input-area">
                                <textarea
                                  aria-label={loadingLabel}
                                  className="h-[46px] min-h-[46px] w-full resize-none border-0 bg-transparent p-0 text-[13px] outline-none"
                                  disabled
                                  rows={2}
                                />
                              </div>
                              <div
                                aria-hidden="true"
                                className="agent-gui-node__composer-footer"
                              >
                                <div className="agent-gui-node__composer-footer-left">
                                  <span className="size-7 rounded-md bg-[var(--transparency-block)]" />
                                  <span className="h-7 w-20 rounded-md bg-[var(--transparency-block)]" />
                                </div>
                                <div className="agent-gui-node__composer-footer-right">
                                  <span className="h-7 w-16 rounded-md bg-[var(--transparency-block)]" />
                                  <Spinner
                                    className="text-[var(--text-tertiary)]"
                                    size={16}
                                  />
                                </div>
                              </div>
                            </div>
                          </div>
                          <div className="agent-gui-node__composer-project-row">
                            <span
                              aria-hidden="true"
                              className="h-7 w-28 shrink-0 rounded-md bg-[var(--transparency-block)]"
                            />
                            <div className="agent-gui-node__composer-prompt-tips">
                              <span className="agent-gui-node__composer-prompt-tip">
                                <span className="agent-gui-node__composer-prompt-tip-track">
                                  <span className="agent-gui-node__composer-prompt-tip-item">
                                    <span
                                      aria-hidden="true"
                                      className="inline-block h-2.5 w-56 rounded-full bg-[var(--transparency-block)]"
                                    />
                                  </span>
                                </span>
                              </span>
                            </div>
                          </div>
                        </div>
                        <div className="agent-gui-node__empty-hero-suggestions">
                          <div
                            aria-hidden="true"
                            className="agent-gui-node__empty-hero-suggestions-chips"
                          >
                            <span className="h-8 w-24 rounded-full bg-[var(--transparency-block)]" />
                            <span className="h-8 w-32 rounded-full bg-[var(--transparency-block)]" />
                            <span className="h-8 w-30 rounded-full bg-[var(--transparency-block)]" />
                            <span className="h-8 w-36 rounded-full bg-[var(--transparency-block)]" />
                            <span className="h-8 w-28 rounded-full bg-[var(--transparency-block)]" />
                          </div>
                        </div>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </section>
        </div>
      </div>
    </div>
  );
}
