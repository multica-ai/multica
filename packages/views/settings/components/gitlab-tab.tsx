"use client";

import { useState, type FormEvent } from "react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspaceGitlabConnection } from "@multica/core/gitlab/queries";
import {
  useConnectWorkspaceGitlabMutation,
  useDisconnectWorkspaceGitlabMutation,
} from "@multica/core/gitlab/mutations";
import { ApiError } from "@multica/core/api";

export function GitlabTab() {
  const wsId = useWorkspaceId();
  const { data, error, isLoading } = useWorkspaceGitlabConnection(wsId);
  const connectMu = useConnectWorkspaceGitlabMutation(wsId);
  const disconnectMu = useDisconnectWorkspaceGitlabMutation(wsId);

  const [project, setProject] = useState("");
  const [token, setToken] = useState("");

  if (isLoading) {
    return <div className="text-muted-foreground text-sm">Loading…</div>;
  }

  // 404 = "not connected"; any other error is a real failure.
  const notConnected =
    !data && error instanceof ApiError && error.status === 404;
  const otherError = !data && !notConnected && error != null;

  if (data && data.connection_status === "connected") {
    return (
      <div className="space-y-4">
        <h2 className="text-xl font-semibold">GitLab</h2>
        <Card>
          <CardContent className="space-y-3 pt-6">
            <div>
              <span className="text-muted-foreground">Project: </span>
              <span className="font-medium">{data.gitlab_project_path}</span>
            </div>
            <div className="text-muted-foreground text-sm">
              Service account user id: {data.service_token_user_id}
            </div>
            <Button
              variant="destructive"
              disabled={disconnectMu.isPending}
              onClick={() => disconnectMu.mutate()}
            >
              {disconnectMu.isPending ? "Disconnecting…" : "Disconnect"}
            </Button>
          </CardContent>
        </Card>
      </div>
    );
  }

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault();
    connectMu.mutate({ project, token });
  };

  return (
    <form className="space-y-4" onSubmit={handleSubmit}>
      <h2 className="text-xl font-semibold">Connect GitLab</h2>
      {otherError ? (
        <div className="text-destructive text-sm">
          Failed to load connection status.
        </div>
      ) : null}
      <div className="space-y-2">
        <Label htmlFor="gitlab-project">Project</Label>
        <Input
          id="gitlab-project"
          value={project}
          onChange={(e) => setProject(e.target.value)}
          placeholder="group/project or numeric ID"
          required
        />
      </div>
      <div className="space-y-2">
        <Label htmlFor="gitlab-token">Service access token</Label>
        <Input
          id="gitlab-token"
          type="password"
          value={token}
          onChange={(e) => setToken(e.target.value)}
          placeholder="glpat-…"
          required
        />
      </div>
      {connectMu.isError ? (
        <div className="text-destructive text-sm">
          {connectMu.error instanceof ApiError
            ? connectMu.error.message
            : "Connection failed"}
        </div>
      ) : null}
      <Button type="submit" disabled={connectMu.isPending || !project || !token}>
        {connectMu.isPending ? "Connecting…" : "Connect"}
      </Button>
    </form>
  );
}
