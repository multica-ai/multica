package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetAnalyticsCatalogReturnsContract(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/catalog", nil)
	recorder := httptest.NewRecorder()

	(&Handler{}).GetAnalyticsCatalog(recorder, req)

	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"quality_pass_rate"`) || !strings.Contains(recorder.Body.String(), `"not_in"`) {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestQueryAnalyticsRejectsMalformedJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/analytics/query", strings.NewReader(`{"population":`))
	recorder := httptest.NewRecorder()

	(&Handler{}).QueryAnalytics(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}
