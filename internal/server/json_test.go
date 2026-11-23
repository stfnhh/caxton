package server

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"caxton/internal/library"
)

func TestEncodeJSONPublicationUsesOPDSImagesCollection(t *testing.T) {
	item := library.Publication{
		ID: "abc", Title: "A Book", Authors: []string{"A. Writer"}, Genres: []string{"Fantasy"},
		Series: "A Series", Modified: time.Unix(1, 0), MediaType: "application/epub+zip", Kind: "ebook",
		Size: 123, CoverType: "image/jpeg",
	}
	got := encodeJSONPublication("https://books.example", item)
	if len(got.Images) != 1 || got.Images[0].Href != "https://books.example/covers/abc" {
		t.Fatalf("unexpected images: %#v", got.Images)
	}
	if len(got.Links) != 2 || got.Links[0].Rel != "self" || got.Links[0].Type != jsonPublicationType {
		t.Fatalf("missing publication self link: %#v", got.Links)
	}
	if got.Links[1].Href != "https://books.example/books/abc" {
		t.Fatalf("invalid acquisition link: %#v", got.Links[1])
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
	if got := encodeJSONPublication("https://books.example", item); got.Metadata["@type"] != "http://schema.org/Audiobook" {
		t.Fatalf("unexpected audiobook type: %#v", got.Metadata["@type"])
	}
}

func TestEmptyJSONFeedsRetainTheirCollection(t *testing.T) {
	handler := New(&library.Catalog{}, "https://books.example")
	for _, path := range []string{"/opds/v2/authors", "/opds/v2/search?query=missing"} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
			var feed map[string]json.RawMessage
			if err := json.Unmarshal(w.Body.Bytes(), &feed); err != nil {
				t.Fatal(err)
			}
			collection := "publications"
			if path == "/opds/v2/authors" {
				collection = "navigation"
			}
			if value, ok := feed[collection]; !ok || string(value) != "[]" {
				t.Fatalf("missing empty %s collection: %s", collection, w.Body.String())
			}
		})
	}
}

func TestOPDS2Routes(t *testing.T) {
	handler := New(&library.Catalog{}, "https://books.example")
	tests := map[string]string{
		"/opds/v2":                    "navigation",
		"/opds/v2/books":              "publications",
		"/opds/v2/recent":             "publications",
		"/opds/v2/authors":            "navigation",
		"/opds/v2/genres":             "navigation",
		"/opds/v2/ebooks":             "publications",
		"/opds/v2/audiobooks":         "publications",
		"/opds/v2/search?query=hello": "publications",
	}
	for path, collection := range tests {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
			var feed map[string]json.RawMessage
			if w.Code != 200 || w.Header().Get("Content-Type") != jsonFeedType {
				t.Fatalf("unexpected response: %d %s", w.Code, w.Body.String())
			}
			if err := json.Unmarshal(w.Body.Bytes(), &feed); err != nil {
				t.Fatal(err)
			}
			if _, ok := feed[collection]; !ok {
				t.Fatalf("missing %s collection: %s", collection, w.Body.String())
			}
		})
	}
}
