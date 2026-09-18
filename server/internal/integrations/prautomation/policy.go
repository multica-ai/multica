// Package prautomation defines the user-visible PR policy without provider or
// database dependencies. Linking and completion share this single evaluator.
package prautomation

import (
	"regexp"
	"sort"
	"strings"
)

type Policy struct {
	Source       string `json:"source"`
	AutoComplete bool   `json:"auto_complete"`
	Revision     int64  `json:"revision"`
}

func (p Policy) Valid() bool {
	return p.Source == "manual" || p.Source == "title_branch" || p.Source == "all"
}

var identifier = regexp.MustCompile(`(?i)\b[a-z][a-z0-9]{0,9}-[0-9]+\b`)

func Identifiers(p Policy, title, branch, body string) map[string]string {
	result := map[string]string{}
	if p.Source == "manual" {
		return result
	}
	for _, field := range []struct{ name, text string }{{"title", title}, {"branch", branch}, {"body", body}} {
		if field.name == "body" && p.Source != "all" {
			continue
		}
		for _, id := range identifier.FindAllString(field.text, -1) {
			key := strings.ToUpper(id)
			if _, ok := result[key]; !ok {
				result[key] = field.name
			}
		}
	}
	return result
}

type Link struct {
	PRID      string `json:"pr_id"`
	Provider  string `json:"provider"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	State     string `json:"state"`
	Source    string `json:"source"`
	Connected bool   `json:"connected"`
}

type Decision struct {
	Reason   string   `json:"reason"`
	Complete bool     `json:"complete"`
	Waiting  []string `json:"waiting"`
}

func Decide(p Policy, terminal, triage, disabled bool, links []Link) Decision {
	d := Decision{Waiting: []string{}}
	switch {
	case terminal:
		d.Reason = "terminal"
	case triage:
		d.Reason = "triage"
	case disabled:
		d.Reason = "issue_disabled"
	case !p.AutoComplete:
		d.Reason = "workspace_disabled"
	case len(links) == 0:
		d.Reason = "no_links"
	default:
		d.Reason = "ready"
		for _, l := range links {
			if !l.Connected || l.Source == "pending" {
				d.Reason = "sync_required"
				d.Waiting = append(d.Waiting, l.PRID)
			} else if l.State != "merged" {
				if d.Reason != "sync_required" {
					d.Reason = "waiting"
				}
				d.Waiting = append(d.Waiting, l.PRID)
			}
		}
		d.Complete = d.Reason == "ready"
	}
	sort.Strings(d.Waiting)
	return d
}
