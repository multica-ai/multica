package lark

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type referenceLinkQueries interface {
	GetWorkspace(context.Context, pgtype.UUID) (db.Workspace, error)
}

// linkReplyReferences formats the outbound copy using workspace metadata.
// Target existence is handled by the destination pages.
func (p *Patcher) linkReplyReferences(ctx context.Context, workspaceID pgtype.UUID, content string) string {
	appURL, err := url.Parse(p.cfg.AppURL)
	if err != nil || (appURL.Scheme != "http" && appURL.Scheme != "https") || appURL.Host == "" || !workspaceID.Valid {
		return content
	}
	var ws db.Workspace
	var workspaceErr error
	loaded := false
	return channel.LinkReferences(content, func(kind, reference string) string {
		switch kind {
		case "issue", "project", "agent", "squad", "member":
		default:
			return ""
		}
		prefix := ""
		if id, err := util.ParseUUID(reference); err == nil {
			reference = util.UUIDToString(id)
		} else {
			if kind != "issue" {
				return ""
			}
			var numberText string
			prefix, numberText, _ = strings.Cut(reference, "-")
			number, err := strconv.ParseInt(numberText, 10, 32)
			if prefix == "" || err != nil || number <= 0 || fmt.Sprintf("%s-%d", prefix, number) != reference {
				return ""
			}
		}
		if !loaded {
			loaded = true
			ws, workspaceErr = p.queries.GetWorkspace(ctx, workspaceID)
		}
		if workspaceErr != nil || ws.ID != workspaceID || ws.Slug == "" || (prefix != "" && prefix != ws.IssuePrefix) {
			return ""
		}
		return channel.ReferenceWebLink(p.cfg.AppURL, ws.Slug, kind, reference)
	})
}
