package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "comment delivery restart helper failed")
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 5 || os.Getenv("DATABASE_URL") == "" {
		return fmt.Errorf("explicit database and fixture arguments required")
	}
	workspaceID, err := util.ParseUUID(os.Args[2])
	if err != nil {
		return err
	}
	commentID, err := util.ParseUUID(os.Args[3])
	if err != nil {
		return err
	}
	agentID, err := util.ParseUUID(os.Args[4])
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer pool.Close()
	queries := db.New(pool)
	hub := realtime.NewHub()
	go hub.Run()
	h := handler.New(queries, pool, hub, events.New(), service.NewEmailService(), nil, nil, analytics.NoopClient{}, handler.Config{})
	switch os.Args[1] {
	case "admit":
		comment, err := queries.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{ID: commentID, WorkspaceID: workspaceID})
		if err != nil {
			return err
		}
		body, err := json.Marshal(map[string]any{
			"content":           fmt.Sprintf("[@Target](mention://agent/%s) %s", util.UUIDToString(agentID), comment.Content),
			"expected_revision": comment.Revision,
		})
		if err != nil {
			return err
		}
		req := httptest.NewRequest(http.MethodPatch, "/api/comments/"+os.Args[3], bytes.NewReader(body)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-User-ID", util.UUIDToString(comment.AuthorID))
		req.Header.Set("X-Workspace-ID", util.UUIDToString(workspaceID))
		route := chi.NewRouteContext()
		route.URLParams.Add("commentId", os.Args[3])
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, route))
		recorder := httptest.NewRecorder()
		h.UpdateComment(recorder, req)
		if recorder.Code != http.StatusOK {
			return fmt.Errorf("accepted edit returned %d", recorder.Code)
		}
		var response handler.CommentResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			return err
		}
		for _, outcome := range response.TriggerOutcomes {
			if outcome.TargetID == os.Args[4] && outcome.Status == handler.DispatchDeferred {
				fmt.Println("accepted_deferred")
				return nil
			}
		}
		return fmt.Errorf("accepted edit did not defer the fixture target")
	case "drain":
		return h.DrainCommentDeliveryObligations(ctx, 50)
	default:
		return fmt.Errorf("unknown fixture operation")
	}
}
