package chatgpt

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
)

type Model struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"display_name"`
}

func (m *Manager) Models(ctx context.Context, expectedClientID string) ([]Model, error) {
	token, err := m.AccessToken(ctx, expectedClientID)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, Resource+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	if err = m.AssertActive(ctx, token); err != nil {
		return nil, err
	}
	response, err := m.client.Do(req)
	if err != nil {
		return nil, &Error{Code: "catalog_unavailable", Retryable: true, Message: "Could not load the selected ChatGPT account's models. Try again."}
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return nil, &Error{Code: "catalog_unavailable", Retryable: true, Message: "ChatGPT model discovery was interrupted."}
	}
	if response.StatusCode != 200 {
		return nil, responseError(response.StatusCode, data, response.Header.Get("x-request-id"))
	}
	var catalog struct {
		Models *[]struct {
			Slug        string `json:"slug"`
			DisplayName string `json:"display_name"`
			Visibility  string `json:"visibility"`
		} `json:"models"`
	}
	if json.Unmarshal(data, &catalog) != nil || catalog.Models == nil {
		return nil, &Error{Code: "catalog_invalid", Message: "ChatGPT returned an invalid model catalog."}
	}
	models := []Model{}
	for _, model := range *catalog.Models {
		if model.Visibility != "list" {
			continue
		}
		if model.Slug == "" || model.DisplayName == "" {
			return nil, &Error{Code: "catalog_invalid", Message: "ChatGPT returned an invalid model catalog entry."}
		}
		models = append(models, Model{Slug: model.Slug, DisplayName: model.DisplayName})
	}
	if err = m.AssertActive(ctx, token); err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return nil, &Error{Code: "catalog_empty", Message: "No models are available for this ChatGPT registration."}
	}
	return models, nil
}
