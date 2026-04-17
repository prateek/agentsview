package server

import (
	"net/http"
	"strings"

	"github.com/wesm/agentsview/internal/db"
)

type searchResponse struct {
	Query   string            `json:"query"`
	Results []db.SearchResult `json:"results"`
	Count   int               `json:"count"`
	Next    int               `json:"next"`
}

// validateSort returns "recency" only for the exact string "recency";
// all other values (including empty) return "relevance".
// This is the same whitelist guard used inside db.Search() before
// ORDER BY interpolation.
func validateSort(s string) string {
	if s == "recency" {
		return "recency"
	}
	return "relevance"
}

// prepareFTSQuery preserves the repo's default exact-phrase behavior
// for multi-word queries while also quoting hyphenated terms such
// as qa-check-1 so SQLite FTS treats them literally.
func prepareFTSQuery(raw string) string {
	switch {
	case raw == "":
		return raw
	case isQuotedFTSPhrase(raw):
		return raw
	case strings.Contains(raw, " ") &&
		!strings.HasPrefix(raw, `"`):
		return `"` + raw + `"`
	case strings.ContainsRune(raw, '-'):
		return `"` + raw + `"`
	default:
		return raw
	}
}

func isQuotedFTSPhrase(raw string) bool {
	return len(raw) >= 2 &&
		strings.HasPrefix(raw, `"`) &&
		strings.HasSuffix(raw, `"`)
}

func (s *Server) handleSearch(
	w http.ResponseWriter, r *http.Request,
) {
	q := r.URL.Query()

	query := strings.TrimSpace(q.Get("q"))
	if query == "" {
		writeError(w, http.StatusBadRequest, "query required")
		return
	}

	limit, ok := parseIntParam(w, r, "limit")
	if !ok {
		return
	}
	limit = clampLimit(limit, db.DefaultSearchLimit, db.MaxSearchLimit)

	cursor, ok := parseIntParam(w, r, "cursor")
	if !ok {
		return
	}

	sort := validateSort(q.Get("sort"))

	if !s.db.HasFTS() {
		writeError(w, http.StatusNotImplemented, "search not available")
		return
	}

	filter := db.SearchFilter{
		Query:   prepareFTSQuery(query),
		Project: q.Get("project"),
		Sort:    sort,
		Cursor:  cursor,
		Limit:   limit,
	}

	page, err := s.db.Search(r.Context(), filter)
	if err != nil {
		if handleContextError(w, err) {
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	results := page.Results
	if results == nil {
		results = []db.SearchResult{}
	}
	writeJSON(w, http.StatusOK, searchResponse{
		Query:   query,
		Results: results,
		Count:   len(results),
		Next:    page.NextCursor,
	})
}
