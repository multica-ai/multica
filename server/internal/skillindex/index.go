package skillindex

import (
	"embed"
	"encoding/json"
	"fmt"
)

//go:embed index.json
var files embed.FS

type Entry struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	SourceURL   string   `json:"source_url"`
	Tags        []string `json:"tags"`
	Category    string   `json:"category,omitempty"`
}

func List() ([]Entry, error) {
	data, err := files.ReadFile("index.json")
	if err != nil {
		return nil, fmt.Errorf("read skill index: %w", err)
	}
	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse skill index: %w", err)
	}
	return entries, nil
}

func MustJSON() string {
	entries, err := List()
	if err != nil {
		panic(err)
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		panic(err)
	}
	return string(data)
}

func SourceURLSet() (map[string]Entry, error) {
	entries, err := List()
	if err != nil {
		return nil, err
	}
	out := make(map[string]Entry, len(entries))
	for _, entry := range entries {
		if entry.SourceURL == "" {
			return nil, fmt.Errorf("skill index entry %q has empty source_url", entry.Name)
		}
		out[entry.SourceURL] = entry
	}
	return out, nil
}
