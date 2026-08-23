package server

import (
	"net/http/httptest"
	"testing"
	"time"

	"caxton/internal/library"
)

func TestEncodePublicationUsesOPDSImagesCollection(t *testing.T) {
	item := library.Publication{
		ID: "abc", Title: "A Book", Authors: []string{"A. Writer"}, Genres: []string{"Fantasy"},
		Series: "A Series", Modified: time.Unix(1, 0), MediaType: "application/epub+zip", Kind: "ebook",
		Size: 123, CoverType: "image/jpeg",
	}
	got := encodePublication("https://books.example", item)
	if len(got.Images) != 1 || got.Images[0].Href != "https://books.example/covers/abc" {
		t.Fatalf("unexpected images: %#v", got.Images)
	}
	if len(got.Links) != 1 {
		t.Fatalf("cover leaked into acquisition links: %#v", got.Links)
	}
	if got.Metadata["subject"].([]string)[0] != "Fantasy" {
		t.Fatalf("missing subjects: %#v", got.Metadata)
	}
	if _, ok := got.Metadata["belongsTo"]; !ok {
		t.Fatalf("missing series: %#v", got.Metadata)
	}
	if got.Metadata["@type"] != "http://schema.org/EBook" {
		t.Fatalf("unexpected publication type: %#v", got.Metadata["@type"])
	}
	item.Kind = "audiobook"
	if got := encodePublication("https://books.example", item); got.Metadata["@type"] != "http://schema.org/Audiobook" {
		t.Fatalf("unexpected audiobook type: %#v", got.Metadata["@type"])
	}
}

func TestPagination(t *testing.T) {
	items := make([]library.Publication, 125)
	r := httptest.NewRequest("GET", "http://example.test/opds/books?genre=Fantasy&page=2&per_page=50", nil)
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
