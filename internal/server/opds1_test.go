package server

import (
	"encoding/json"
	"encoding/xml"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"caxton/internal/library"
)

func TestOPDS1Routes(t *testing.T) {
	handler := New(&library.Catalog{}, "https://books.example/library")
	for _, path := range []string{"", "/books", "/recent", "/authors", "/genres", "/ebooks", "/audiobooks", "/search?query=hello"} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest("GET", "http://localhost/opds1"+path, nil))
			var f atomFeed
			if w.Code != 200 || w.Header().Get("Content-Type") != atomType {
				t.Fatalf("unexpected response: %d %s", w.Code, w.Body.String())
			}
			if err := xml.Unmarshal(w.Body.Bytes(), &f); err != nil {
				t.Fatal(err)
			}
			if f.ID == "" || f.Title == "" || f.Updated == "" || f.Author.Name == "" {
				t.Fatalf("missing Atom metadata: %#v", f)
			}
			for _, link := range append(f.Links, entryLinks(f.Entries)...) {
				if !strings.HasPrefix(link.Href, "https://books.example/library/opds1") {
					t.Errorf("wrong catalog link: %s", link.Href)
				}
			}
			if path == "" && len(f.Entries) != 6 {
				t.Fatalf("expected six navigation entries: %#v", f.Entries)
			}
		})
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "http://localhost/opds", nil))
	if w.Header().Get("Content-Type") != feedType || !json.Valid(w.Body.Bytes()) {
		t.Fatal("OPDS 2 feed changed")
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "http://localhost/opds1/missing", nil))
	if w.Code != 404 {
		t.Fatalf("unknown route status: %d", w.Code)
	}
}

func entryLinks(entries []atomEntry) []atomLink {
	var links []atomLink
	for _, entry := range entries {
		links = append(links, entry.Links...)
	}
	return links
}

func TestOPDS1PublicationAndPagination(t *testing.T) {
	s := &Server{baseURL: "https://books.example/prefix"}
	date := time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)
	for _, mediaType := range []string{"application/epub+zip", "audio/mp4"} {
		item := library.Publication{ID: "abc", Title: "A & <Book>", Authors: []string{"A & B", "C"}, Genres: []string{"Fantasy"}, Description: "Words & <markup>", Modified: date, Published: &date, MediaType: mediaType, Size: 123, CoverType: "image/jpeg", Language: "en", Publisher: "Publisher", Series: "Series", Duration: 100}
		r := httptest.NewRequest("GET", "http://localhost/opds1/books?author=A%26B&page=2&per_page=1", nil)
		w := httptest.NewRecorder()
		s.writeAtomPublications(w, r, "Books", []library.Publication{item}, 2, 1, 3)
		var f atomFeed
		if err := xml.Unmarshal(w.Body.Bytes(), &f); err != nil {
			t.Fatal(err)
		}
		if len(f.Entries) != 1 {
			t.Fatal("missing book")
		}
		e := f.Entries[0]
		if e.Title != item.Title || e.Summary.Text != item.Description || e.Summary.Type != "text" || len(e.Authors) != 2 || e.ID != "urn:caxton:abc" || e.Updated != date.Format(time.RFC3339) || e.Issued != date.Format(time.RFC3339) || e.Language != "en" || e.Publisher != "Publisher" || e.Categories[0].Term != "Fantasy" {
			t.Fatalf("metadata did not round trip: %#v", e)
		}
		if e.Links[0].Type != mediaType || e.Links[0].Length != 123 || e.Links[0].Href != s.baseURL+"/books/abc" || e.Links[0].Rel != "http://opds-spec.org/acquisition/open-access" || e.Links[1].Rel != "http://opds-spec.org/image" {
			t.Fatalf("wrong download/cover: %#v", e.Links)
		}
		seen := map[string]bool{}
		for _, link := range f.Links {
			seen[link.Rel] = true
			if link.Rel == "next" || link.Rel == "prev" || link.Rel == "first" || link.Rel == "last" {
				if !strings.Contains(link.Href, "/opds1/books?") || !strings.Contains(link.Href, "author=A%26B") {
					t.Fatalf("lost route/filter: %s", link.Href)
				}
			}
		}
		for _, rel := range []string{"self", "start", "search", "first", "last", "prev", "next"} {
			if !seen[rel] {
				t.Errorf("missing %s link", rel)
			}
		}
	}
}

func TestOPDS1SearchDiscovery(t *testing.T) {
	handler := New(&library.Catalog{}, "https://books.example")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "http://localhost/opds1/search.xml", nil))
	var description struct {
		XMLName xml.Name
		URL     struct {
			Type     string `xml:"type,attr"`
			Template string `xml:"template,attr"`
		} `xml:"Url"`
	}
	if err := xml.Unmarshal(w.Body.Bytes(), &description); err != nil {
		t.Fatal(err)
	}
	if w.Header().Get("Content-Type") != openSearchType || description.XMLName.Space != "http://a9.com/-/spec/opensearch/1.1/" || description.URL.Type != atomType {
		t.Fatalf("invalid search description: %s", w.Body.String())
	}
	url := strings.ReplaceAll(description.URL.Template, "{searchTerms}", "fantasy")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", url, nil))
	if w.Code != 200 || w.Header().Get("Content-Type") != atomType {
		t.Fatal("search template does not return Atom")
	}
}

func TestOPDS1AuthorNavigation(t *testing.T) {
	s := &Server{baseURL: "https://books.example"}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "http://localhost/opds1/authors", nil)
	s.navigationFeed(w, r, "Authors", []string{"A & B"}, "author")
	var f atomFeed
	if err := xml.Unmarshal(w.Body.Bytes(), &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Entries) != 1 || f.Entries[0].Title != "A & B" || f.Entries[0].Links[0].Href != "https://books.example/opds1/books?author=A+%26+B" {
		t.Fatalf("invalid author navigation: %#v", f.Entries)
	}
}
