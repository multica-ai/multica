"use client";

import { useState, useCallback } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Pencil, Check, X, Search } from "lucide-react";
import { useT } from "../i18n";
import { userListOptions } from "@multica/core/admin";
import { useUpdateUserName } from "@multica/core/admin";
import type { AdminUser } from "@multica/core/types";
import { Input } from "@multica/ui/input";
import { Button } from "@multica/ui/button";

function UserRow({
  user,
  onRename,
}: {
  user: AdminUser;
  onRename: (userId: string, name: string) => Promise<void>;
}) {
  const { t } = useT("admin");
  const [editing, setEditing] = useState(false);
  const [name, setName] = useState(user.name);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  const handleSave = async () => {
    const trimmed = name.trim();
    if (!trimmed) {
      setError(t(($) => $.rename_empty_error));
      return;
    }
    setSaving(true);
    setError("");
    try {
      await onRename(user.id, trimmed);
      setEditing(false);
    } catch {
      setError(t(($) => $.rename_error));
    } finally {
      setSaving(false);
    }
  };

  const handleCancel = useCallback(() => {
    setName(user.name);
    setError("");
    setEditing(false);
  }, [user.name]);

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter") handleSave();
    if (e.key === "Escape") handleCancel();
  };

  return (
    <div className="flex items-center gap-4 px-4 py-3">
      <div className="min-w-0 flex-1">
        {editing ? (
          <div>
            <Input
              autoFocus
              value={name}
              onChange={(e) => setName(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder={t(($) => $.rename_placeholder)}
              className="h-7 text-sm"
              aria-label={t(($) => $.rename_placeholder)}
            />
            {error && <p className="mt-1 text-xs text-destructive">{error}</p>}
          </div>
        ) : (
          <div className="text-sm font-medium truncate">{user.name}</div>
        )}
        <div className="text-xs text-muted-foreground truncate">{user.email}</div>
      </div>
      <div className="flex items-center gap-1 shrink-0">
        {editing ? (
          <>
            <Button
              size="sm"
              variant="ghost"
              className="h-7 w-7 p-0"
              onClick={handleSave}
              disabled={saving}
              aria-label={t(($) => $.rename_save)}
            >
              <Check className="h-3.5 w-3.5" />
            </Button>
            <Button
              size="sm"
              variant="ghost"
              className="h-7 w-7 p-0"
              onClick={handleCancel}
              disabled={saving}
              aria-label={t(($) => $.rename_cancel)}
            >
              <X className="h-3.5 w-3.5" />
            </Button>
          </>
        ) : (
          <Button
            size="sm"
            variant="ghost"
            className="h-7 w-7 p-0"
            onClick={() => setEditing(true)}
            aria-label={t(($) => $.rename_button)}
          >
            <Pencil className="h-3.5 w-3.5" />
          </Button>
        )}
      </div>
    </div>
  );
}

export function UserManagementPage() {
  const { t } = useT("admin");
  const [search, setSearch] = useState("");
  const { data: users = [], isLoading } = useQuery(userListOptions({ search }));
  const updateName = useUpdateUserName();

  const handleRename = async (userId: string, name: string) => {
    await updateName.mutateAsync({ userId, name });
    toast.success(t(($) => $.rename_success));
  };

  return (
    <div className="mx-auto max-w-2xl space-y-4 p-6">
      <h1 className="text-xl font-semibold">{t(($) => $.page_title)}</h1>
      <div className="relative">
        <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          className="pl-9"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder={t(($) => $.search_placeholder)}
          aria-label={t(($) => $.search_placeholder)}
        />
      </div>
      {isLoading ? (
        <div className="space-y-2">
          {[1, 2, 3].map((i) => (
            <div key={i} className="h-14 rounded-xl bg-muted animate-pulse" />
          ))}
        </div>
      ) : users.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t(($) => $.empty)}</p>
      ) : (
        <div className="overflow-hidden rounded-xl ring-1 ring-foreground/10">
          {users.map((user, i) => (
            <div key={user.id} className={i > 0 ? "border-t border-border/50" : ""}>
              <UserRow user={user} onRename={handleRename} />
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
