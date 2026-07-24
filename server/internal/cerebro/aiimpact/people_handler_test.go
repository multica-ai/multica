package aiimpact

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestListPeopleImpactReturnsRealMemberAndAgentUsage(t *testing.T) {
	memberID := uuid.New()
	agentID := uuid.New()
	quality := 0.91
	confidence := 0.78
	cost := int64(125)
	store := &recordingObservationStore{
		people: []PersonImpact{
			{
				ID:   memberID,
				Type: "member",
				Name: "Maya",
				Activity: []PeopleActivityBucket{
					{Bucket: time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC), Count: 4},
				},
				Usage: PeopleUsage{Runs: 4, Issues: 3, Projects: 1, Chats: 2, Channels: 1},
				Outcomes: PeopleOutcomes{
					SolutionQuality: &quality,
					SkillActivity:   3,
					CostCents:       &cost,
				},
				Confidence: &confidence,
				SampleSize: 8,
			},
			{ID: agentID, Type: "agent", Name: "Lone"},
		},
	}
	handler := NewHandler(NewService(store))
	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			workspaceID := uuid.New()
			userID := uuid.New()
			ctx := middleware.SetMemberContext(r.Context(), workspaceID.String(), db.Member{
				UserID: pgtype.UUID{Bytes: [16]byte(userID), Valid: true},
				Role:   "member",
			})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	handler.Mount(router)

	request := httptest.NewRequest(http.MethodGet, "/api/cerebro/ai-impact/people?period=hour", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		Period PeoplePeriod `json:"period"`
		People []struct {
			ID         uuid.UUID `json:"id"`
			Type       string    `json:"type"`
			Name       string    `json:"name"`
			SampleSize int64     `json:"sample_size"`
			Usage      struct {
				Runs     int64 `json:"runs"`
				Issues   int64 `json:"issues"`
				Projects int64 `json:"projects"`
				Chats    int64 `json:"chats"`
				Channels int64 `json:"channels"`
			} `json:"usage"`
			Outcomes struct {
				NeedsSolved     *NeedsSolved `json:"needs_solved"`
				SolutionQuality *float64     `json:"solution_quality"`
				SkillActivity   int64        `json:"skill_activity"`
				CostCents       *int64       `json:"cost_cents"`
			} `json:"outcomes"`
		} `json:"people"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Period != PeoplePeriodHour || len(payload.People) != 2 {
		t.Fatalf("payload = %+v", payload)
	}
	member := payload.People[0]
	if member.ID != memberID || member.Type != "member" || member.Name != "Maya" {
		t.Fatalf("member = %+v", member)
	}
	if member.Usage != (struct {
		Runs     int64 `json:"runs"`
		Issues   int64 `json:"issues"`
		Projects int64 `json:"projects"`
		Chats    int64 `json:"chats"`
		Channels int64 `json:"channels"`
	}{Runs: 4, Issues: 3, Projects: 1, Chats: 2, Channels: 1}) {
		t.Fatalf("usage = %+v", member.Usage)
	}
	if member.Outcomes.NeedsSolved != nil || member.Outcomes.SolutionQuality == nil ||
		*member.Outcomes.SolutionQuality != quality || member.Outcomes.SkillActivity != 3 ||
		member.Outcomes.CostCents == nil || *member.Outcomes.CostCents != cost ||
		member.SampleSize != 8 {
		t.Fatalf("outcomes = %+v, sample_size = %d", member.Outcomes, member.SampleSize)
	}
}

func TestListPeopleImpactRejectsUnknownPeriod(t *testing.T) {
	handler := NewHandler(NewService(&recordingObservationStore{}))
	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			workspaceID := uuid.New()
			userID := uuid.New()
			ctx := middleware.SetMemberContext(r.Context(), workspaceID.String(), db.Member{
				UserID: pgtype.UUID{Bytes: [16]byte(userID), Valid: true},
				Role:   "member",
			})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	handler.Mount(router)

	request := httptest.NewRequest(http.MethodGet, "/api/cerebro/ai-impact/people?period=year", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", response.Code, response.Body.String())
	}
}
