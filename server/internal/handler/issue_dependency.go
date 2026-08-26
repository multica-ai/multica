package handler
import (
	"net/http"
	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)
func (h *Handler) CreateIssueDependency(w http.ResponseWriter,r *http.Request){
	issue,ok:=h.loadIssueForUser(w,r,chi.URLParam(r,"id"));if!ok{return}
	dep:=r.URL.Query().Get("depends_on");du,ok:=parseUUIDOrBadRequest(w,dep,"depends_on");if!ok{return};if uuidToString(issue.ID)==dep{writeError(w,400,"cannot depend on self");return}
	if _,err:=h.Queries.GetIssueInWorkspace(r.Context(),db.GetIssueInWorkspaceParams{ID:du,WorkspaceID:issue.WorkspaceID});err!=nil{writeError(w,404,"depends_on issue not found");return}
	row,err:=h.Queries.CreateIssueDependency(r.Context(),db.CreateIssueDependencyParams{IssueID:issue.ID,DependsOnIssueID:du});if err!=nil{if isUniqueViolation(err){writeError(w,409,"dependency already exists");return};writeError(w,500,"failed to create dependency");return};writeJSON(w,201,row)}
func (h *Handler) DeleteIssueDependency(w http.ResponseWriter,r *http.Request){
	issue,ok:=h.loadIssueForUser(w,r,chi.URLParam(r,"id"));if!ok{return}
	dep:=r.URL.Query().Get("depends_on");du,ok:=parseUUIDOrBadRequest(w,dep,"depends_on");if!ok{return}
	if err:=h.Queries.DeleteIssueDependency(r.Context(),db.DeleteIssueDependencyParams{IssueID:issue.ID,DependsOnIssueID:du});err!=nil{writeError(w,500,"failed to delete dependency");return};w.WriteHeader(204)}
func (h *Handler) ListIssueDependencies(w http.ResponseWriter,r *http.Request){
	issue,ok:=h.loadIssueForUser(w,r,chi.URLParam(r,"id"));if!ok{return}
	rows,err:=h.Queries.ListIssueDependencies(r.Context(),issue.ID);if err!=nil{writeError(w,500,"failed to list dependencies");return};writeJSON(w,200,map[string]any{"dependencies":rows})}
