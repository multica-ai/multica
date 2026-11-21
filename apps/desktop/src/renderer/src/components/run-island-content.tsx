import { ArrowUpRight, CircleDashed, LoaderCircle } from "@multica/ui/icons";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import {
  islandRunGroups,
  type IslandProfile,
  type IslandRun,
} from "../../../shared/run-island";
import "../run-island-content.css";

export function IslandCap({
  runs,
  profile,
  attached,
}: {
  runs: IslandRun[];
  profile: IslandProfile;
  attached: boolean;
}) {
  const first = runs.find((r) => r.status === "running") ?? runs[0];
  const agents = new Map<string, string>();
  for (const run of runs) {
    agents.set(run.agentId, run.name);
    if (agents.size === 3) break;
  }
  return (
    <>
      <MulticaIcon
        size={19}
        mode={first?.status === "running" ? "think" : "static"}
      />
      {!attached && profile.center !== "none" && (
        <span className="island-center">
          {profile.center === "agent" ? first?.name : first?.detail}
        </span>
      )}
      {profile.trailing === "count" && (
        <span className="island-count">
          <i data-status={first?.status} />
          {runs.length}
        </span>
      )}
      {profile.trailing === "agents" && (
        <span className="island-agents">
          {[...agents].map(([id, name]) => (
            <span key={id} title={name}>
              {Array.from(name)[0]}
            </span>
          ))}
        </span>
      )}
    </>
  );
}

export function IslandRunList({
  runs,
  profile,
  openRun,
}: {
  runs: IslandRun[];
  profile: IslandProfile;
  openRun?: (id: string) => void;
}) {
  return (
    <div className="island-list" data-indicator={profile.indicator}>
      {islandRunGroups(runs, profile).map((group) => (
        <div key={group.key}>
          {profile.group !== "none" && (
            <div className="island-group">
              {group.label} <span>{group.runs.length}</span>
            </div>
          )}
          {group.runs.map((run) => {
            const content = (
              <>
                {profile.indicator === "glyph" ? (
                  run.status === "running" ? (
                    <LoaderCircle size={13} />
                  ) : (
                    <CircleDashed size={13} />
                  )
                ) : (
                  <i aria-hidden="true" />
                )}
                <span className="island-description">
                  <b>{run.name}</b>
                  <small>{run.detail}</small>
                </span>
                <span className="island-state">{run.label}</span>
                {openRun && <ArrowUpRight className="island-arrow" size={13} />}
              </>
            );
            return openRun ? (
              <button
                key={run.id}
                className="run"
                data-status={run.status}
                onClick={() => openRun(run.id)}
              >
                {content}
              </button>
            ) : (
              <div key={run.id} className="run" data-status={run.status}>
                {content}
              </div>
            );
          })}
        </div>
      ))}
    </div>
  );
}
