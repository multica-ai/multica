package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// validNotifGroups is the set of notification preference group keys that the
// API accepts. Keys not in this set are rejected. `system_notifications` is
// not an inbox event group — it's a delivery-channel toggle controlling
// whether native OS notification banners fire — but it shares the same
// preferences map so a single endpoint covers all user notification
// preferences.
var validNotifGroups = map[string]bool{
	"assignments":          true,
	"status_changes":       true,
	"comments":             true,
	"updates":              true,
	"agent_activity":       true,
	"system_notifications": true,
}

// validNotifValues is the set of allowed preference values per group.
var validNotifValues = map[string]bool{
	"all":   true,
	"muted": true,
}

var validNotifChannelKeyRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

// validNotifChannelEventKeys is the set of boolean keys under
// preferences.channel.<connection_id>.*. Each key represents one event family
// that can be muted for a concrete channel connection.
//
// Default semantics: a key absent from the map is treated as enabled
// (`true`). This is the contract the API, the UI, and any downstream
// consumer (e.g. the T13 Subscriber) MUST share — see
// IsChannelEventEnabled below for the canonical predicate.
var validNotifChannelEventKeys = map[string]bool{
	"issues":        true,
	"comments":      true,
	"mentions":      true,
	"slash_aliases": true,
}

// IsChannelEventEnabled returns true when the provider integration should
// deliver an event of the given key for the given preferences map.
// Missing keys mean "enabled" (default-on); explicit false means muted.
//
// Centralising this rule prevents drift between the UI ("missing == on")
// and any downstream worker that might otherwise treat missing as off.
func IsChannelEventEnabled(prefs map[string]any, connectionID, key string) bool {
	channel, ok := prefs["channel"].(map[string]any)
	if !ok {
		return true
	}
	providerPrefs, ok := channel[connectionID].(map[string]any)
	if !ok {
		return true
	}
	v, ok := providerPrefs[key]
	if !ok {
		return true
	}
	b, ok := v.(bool)
	if !ok {
		// Defensive: malformed legacy data is treated as enabled rather
		// than silently muting.
		return true
	}
	return b
}

// validatePreferences checks that every key in the incoming preferences map is
// valid. Flat keys must have string values ("all"/"muted"). The special
// "channel" key must be an object whose sub-keys are recognised channel
// names; each channel's leaf values must match the channel value schema.
func validatePreferences(prefs map[string]any) error {
	for k, v := range prefs {
		if k == "channel" {
			channelMap, ok := v.(map[string]any)
			if !ok {
				return fmt.Errorf("channel must be an object, got %T", v)
			}
			for ck, cv := range channelMap {
				if !validNotifChannelKeyRe.MatchString(ck) {
					return fmt.Errorf("invalid channel: %s", ck)
				}
				providerMap, ok := cv.(map[string]any)
				if !ok {
					return fmt.Errorf("channel.%s must be an object, got %T", ck, cv)
				}
				for fk, fv := range providerMap {
					if fk == "slash_aliases" {
						aliases, ok := fv.(map[string]any)
						if !ok {
							return fmt.Errorf("channel.%s.slash_aliases must be an object, got %T", ck, fv)
						}
						for ak, av := range aliases {
							if _, ok := av.(string); !ok {
								return fmt.Errorf("channel.%s.slash_aliases.%s must be a string, got %T", ck, ak, av)
							}
						}
						continue
					}
					if !validNotifChannelEventKeys[fk] {
						return fmt.Errorf("invalid channel.%s key: %s", ck, fk)
					}
					// C3: leaf values must be bool. Letting strings,
					// numbers, or nested objects through here lets the
					// JSONB column accumulate garbage that downstream
					// readers cannot type-assert.
					if _, ok := fv.(bool); !ok {
						return fmt.Errorf("channel.%s.%s must be a bool, got %T", ck, fk, fv)
					}
				}
			}
			continue
		}
		if !validNotifGroups[k] {
			return fmt.Errorf("invalid preference group: %s", k)
		}
		strVal, ok := v.(string)
		if !ok {
			return fmt.Errorf("preference value for %s must be a string, got %T", k, v)
		}
		if !validNotifValues[strVal] {
			return fmt.Errorf("invalid preference value for %s: %s", k, strVal)
		}
	}
	return nil
}

// mergePreferences merges an incoming partial update into the existing
// preferences stored in the DB. Flat string keys are overwritten; the "channel"
// key is deep-merged so that only the sub-keys present in the update replace
// the corresponding sub-keys in the existing map.
//
// The function is non-mutating: nested maps from `existing` are copied into
// freshly allocated maps before being modified, so callers may keep using
// `existing` after the call without observing writes from the merge.
func mergePreferences(existing, incoming map[string]any) map[string]any {
	merged := make(map[string]any, len(existing))
	for k, v := range existing {
		merged[k] = v
	}
	for k, v := range incoming {
		if k == "channel" {
			incomingChannel, ok := v.(map[string]any)
			if !ok {
				merged[k] = v
				continue
			}
			// R2: take a fresh copy of the existing channel map so writes
			// below do not bleed back into `existing`.
			existingChannel, _ := merged[k].(map[string]any)
			newChannel := make(map[string]any, len(existingChannel)+len(incomingChannel))
			for ek, ev := range existingChannel {
				newChannel[ek] = ev
			}
			for ck, cv := range incomingChannel {
				incomingProvider, ok := cv.(map[string]any)
				if !ok {
					newChannel[ck] = cv
					continue
				}
				// R2: same defensive copy at the provider level.
				existingProvider, _ := newChannel[ck].(map[string]any)
				newProvider := make(map[string]any, len(existingProvider)+len(incomingProvider))
				for fk, fv := range existingProvider {
					newProvider[fk] = fv
				}
				for fk, fv := range incomingProvider {
					if fk == "slash_aliases" {
						incomingAliases, ok := fv.(map[string]any)
						if !ok {
							newProvider[fk] = fv
							continue
						}
						existingAliases, _ := newProvider[fk].(map[string]any)
						mergedAliases := make(map[string]any, len(existingAliases)+len(incomingAliases))
						for k, v := range existingAliases {
							mergedAliases[k] = v
						}
						for k, v := range incomingAliases {
							mergedAliases[k] = v
						}
						newProvider[fk] = mergedAliases
						continue
					}
					newProvider[fk] = fv
				}
				newChannel[ck] = newProvider
			}
			merged[k] = newChannel
		} else {
			merged[k] = v
		}
	}
	return merged
}

func (h *Handler) GetNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	pref, err := h.Queries.GetNotificationPreference(r.Context(), db.GetNotificationPreferenceParams{
		WorkspaceID: wsUUID,
		UserID:      parseUUID(userID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusOK, map[string]any{
				"workspace_id": workspaceID,
				"preferences":  map[string]any{},
			})
			return
		}
		slog.Warn("GetNotificationPreference failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to get notification preferences")
		return
	}

	var prefs map[string]any
	if err := json.Unmarshal(pref.Preferences, &prefs); err != nil {
		prefs = map[string]any{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"workspace_id": workspaceID,
		"preferences":  prefs,
	})
}

type updateNotifPrefRequest struct {
	Preferences map[string]any `json:"preferences"`
}

func (h *Handler) UpdateNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	var req updateNotifPrefRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Preferences == nil {
		writeError(w, http.StatusBadRequest, "preferences field is required")
		return
	}

	if err := validatePreferences(req.Preferences); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Fetch existing preferences so we can merge (frontend sends partial updates).
	//
	// R3: distinguish "row missing" (first write — start with empty base)
	// from real DB errors (transient outage, permission, etc.). Treating a
	// real error as "no row" would silently drop every untouched field
	// on the next write because the merge base would be empty — the very
	// hazard the partial-update API is supposed to avoid.
	var merged map[string]any
	existing, err := h.Queries.GetNotificationPreference(r.Context(), db.GetNotificationPreferenceParams{
		WorkspaceID: wsUUID,
		UserID:      parseUUID(userID),
	})
	switch {
	case err == nil:
		if uerr := json.Unmarshal(existing.Preferences, &merged); uerr != nil {
			slog.Warn("existing preferences unmarshal failed; treating as empty",
				append(logger.RequestAttrs(r), "error", uerr)...)
			merged = map[string]any{}
		}
		if merged == nil {
			merged = map[string]any{}
		}
	case errors.Is(err, pgx.ErrNoRows):
		merged = map[string]any{}
	default:
		slog.Warn("GetNotificationPreference failed during update",
			append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load existing preferences")
		return
	}
	merged = mergePreferences(merged, req.Preferences)

	prefsJSON, err := json.Marshal(merged)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to marshal preferences")
		return
	}

	_, err = h.Queries.UpsertNotificationPreference(r.Context(), db.UpsertNotificationPreferenceParams{
		WorkspaceID: wsUUID,
		UserID:      parseUUID(userID),
		Preferences: prefsJSON,
	})
	if err != nil {
		slog.Warn("UpsertNotificationPreference failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update notification preferences")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"workspace_id": workspaceID,
		"preferences":  merged,
	})
}
