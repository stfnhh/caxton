package server

import (
	"net/http/httptest"
	"testing"

	"caxton/internal/library"
)

func TestPagination(t *testing.T) {
	items := make([]library.Publication, 125)
	r := httptest.NewRequest("GET", "http://example.test/opds/v2/books?genre=Fantasy&page=2&per_page=50", nil)
	page, perPage, selected := paginate(items, r)
	if page != 2 || perPage != 50 || len(selected) != 50 {
		t.Fatalf("page=%d perPage=%d len=%d", page, perPage, len(selected))
	}
	links := paginationLinks("http://example.test", r, page, perPage, len(items))
	var hasNext, hasPrevious bool
	for _, item := range links {
		switch rel := item.Rel.(type) {
		case string:
			hasNext = hasNext || rel == "next"
		case []string:
			hasPrevious = hasPrevious || len(rel) > 0 && rel[0] == "previous"
		}
		if item.Rel != "self" && item.Rel != "start" && item.Href != "" && !containsURLValue(item.Href, "genre=Fantasy") {
			t.Fatalf("filter missing from pagination link: %s", item.Href)
		}
	}
	if !hasNext || !hasPrevious {
		t.Fatalf("missing pagination links: %#v", links)
	}
}

func TestUniqueValuesAreCaseInsensitiveAndSorted(t *testing.T) {
	items := []library.Publication{{Authors: []string{"Zed", "alice"}}, {Authors: []string{"Alice", "Bob"}}}
	got := uniqueValues(items, func(item library.Publication) []string { return item.Authors })
	if len(got) != 3 || got[0] != "alice" || got[1] != "Bob" || got[2] != "Zed" {
		t.Fatalf("unexpected values: %#v", got)
	}
}

func containsURLValue(value, wanted string) bool {
	for i := 0; i+len(wanted) <= len(value); i++ {
		if value[i:i+len(wanted)] == wanted {
			return true
		}
	}
	return false
}
