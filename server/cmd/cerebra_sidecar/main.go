package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"

	"github.com/multica-ai/multica/server/internal/cerebra"
)

type routeRequest struct {
	IssueID   string `json:"issue_id"`
	TaskID    string `json:"task_id"`
	SessionID string `json:"session_id"`
	Prompt    string `json:"prompt"`
	UseMCP    bool   `json:"will_use_mcp_tools"`
}

type routeResponse struct {
	RecommendedModel string `json:"recommended_model"`
	Tier             string `json:"tier"`
	MatchedRule      string `json:"matched_rule"`
}

func main() {
	port := os.Getenv("CEREBRA_PORT")
	if port == "" {
		port = "7842"
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	http.HandleFunc("/route", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}

		var req routeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		// Dynamic model discovery — probes Ollama + any locally known runtimes
		models := cerebra.DiscoverModels(r.Context())
		tierMap := cerebra.BuildTierMapFromCatalog(models)

		classifier := cerebra.HeuristicClassifier{}
		policy := &cerebra.Policy{}
		session := cerebra.NewSessionStore(0)
		unavail := cerebra.NewUnavailabilityStore(0)
		router := cerebra.NewRouter(classifier, policy, session, unavail, logger, nil)

		runtimes := []cerebra.RuntimeEntry{{RuntimeID: "cerebra-sidecar", TierMap: tierMap}}
		meta := cerebra.TaskMeta{
			TaskID:          req.TaskID,
			IssueID:         req.IssueID,
			SessionID:       req.SessionID,
			WillUseMCPTools: req.UseMCP,
		}

		result := router.Route(r.Context(), req.Prompt, meta, runtimes, "")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(routeResponse{
			RecommendedModel: result.Model,
			Tier:             string(result.Tier),
			MatchedRule:      result.MatchedRule,
		})

		logger.Info("routed",
			"task_id", req.TaskID,
			"tier", result.Tier,
			"model", result.Model,
			"rule", result.MatchedRule,
		)
	})

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "version": "1.0.0"})
	})

	logger.Info("cerebra sidecar started", "port", port, "route", "/route", "health", "/health")
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		logger.Error("sidecar failed", "error", err)
		os.Exit(1)
	}
}
