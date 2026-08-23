package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"caxton/internal/library"
)

const feedType = "application/opds+json"

type Server struct {
	catalog *library.Catalog
	baseURL string
	mux     *http.ServeMux
}

func New(catalog *library.Catalog, baseURL string) http.Handler {
	s := &Server{catalog: catalog, baseURL: strings.TrimRight(baseURL, "/"), mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /", s.root)
	s.mux.HandleFunc("GET /opds", s.catalogFeed)
	s.mux.HandleFunc("GET /opds/books", s.allPublications)
	s.mux.HandleFunc("GET /opds/recent", s.recent)
	s.mux.HandleFunc("GET /opds/authors", s.authors)
	s.mux.HandleFunc("GET /opds/genres", s.genres)
	s.mux.HandleFunc("GET /opds/search", s.search)
	s.mux.HandleFunc("GET /opds/ebooks", s.ebooks)
	s.mux.HandleFunc("GET /opds/audiobooks", s.audiobooks)
	s.mux.HandleFunc("GET /books/{id}", s.download)
	s.mux.HandleFunc("GET /covers/{id}", s.cover)
	s.mux.HandleFunc("GET /healthz", s.health)
	return s.mux
}

type link struct {
	Href       string         `json:"href"`
	Type       string         `json:"type,omitempty"`
	Rel        any            `json:"rel,omitempty"`
	Title      string         `json:"title,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
	Templated  bool           `json:"templated,omitempty"`
}

type feed struct {
	Metadata     map[string]any `json:"metadata"`
	Links        []link         `json:"links"`
	Navigation   []link         `json:"navigation,omitempty"`
	Publications []publication  `json:"publications,omitempty"`
}

type publication struct {
	Metadata map[string]any `json:"metadata"`
	Links    []link         `json:"links"`
	Images   []link         `json:"images,omitempty"`
}

func (s *Server) root(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/opds", http.StatusTemporaryRedirect)
}

func (s *Server) catalogFeed(w http.ResponseWriter, r *http.Request) {
	base := s.requestBase(r)
	items := s.catalog.Items("")
	s.writeFeed(w, feed{
		Metadata: metadata("Caxton", len(items)),
		Links: []link{
			{Href: base + "/opds", Type: feedType, Rel: "self"},
			{Href: base + "/opds", Type: feedType, Rel: "start"},
			{Href: base + "/opds/search{?query,title,author}", Type: feedType, Rel: "search", Templated: true},
		},
		Navigation: []link{
			{Href: base + "/opds/recent", Type: feedType, Rel: "subsection", Title: "Recently Added"},
			{Href: base + "/opds/books", Type: feedType, Rel: "subsection", Title: "All Books"},
			{Href: base + "/opds/authors", Type: feedType, Rel: "subsection", Title: "Authors"},
			{Href: base + "/opds/genres", Type: feedType, Rel: "subsection", Title: "Genres"},
			{Href: base + "/opds/ebooks", Type: feedType, Rel: "subsection", Title: "eBooks"},
			{Href: base + "/opds/audiobooks", Type: feedType, Rel: "subsection", Title: "Audiobooks"},
		},
	})
}

func (s *Server) ebooks(w http.ResponseWriter, r *http.Request) {
	s.publicationFeed(w, r, "eBooks", s.catalog.Items("ebook"))
}
func (s *Server) audiobooks(w http.ResponseWriter, r *http.Request) {
	s.publicationFeed(w, r, "Audiobooks", s.catalog.Items("audiobook"))
}

func (s *Server) allPublications(w http.ResponseWriter, r *http.Request) {
	items := s.catalog.Items("")
	title := "All Books"
	if author := r.URL.Query().Get("author"); author != "" {
		items = filterItems(items, func(item library.Publication) bool { return containsFold(item.Authors, author) })
		title = "Books by " + author
	}
	if genre := r.URL.Query().Get("genre"); genre != "" {
		items = filterItems(items, func(item library.Publication) bool { return containsFold(item.Genres, genre) })
		title = genre
	}
	s.publicationFeed(w, r, title, items)
}

func (s *Server) recent(w http.ResponseWriter, r *http.Request) {
	items := s.catalog.Items("")
	sort.SliceStable(items, func(i, j int) bool { return items[i].Added.After(items[j].Added) })
	s.publicationFeed(w, r, "Recently Added", items)
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	titleQuery := strings.TrimSpace(r.URL.Query().Get("title"))
	authorQuery := strings.TrimSpace(r.URL.Query().Get("author"))
	items := filterItems(s.catalog.Items(""), func(item library.Publication) bool {
		if titleQuery != "" && !strings.Contains(strings.ToLower(item.Title), strings.ToLower(titleQuery)) {
			return false
		}
		if authorQuery != "" && !containsSubstringFold(item.Authors, authorQuery) {
			return false
		}
		if query == "" {
			return true
		}
		haystack := strings.Join(append(append([]string{item.Title, item.Series}, item.Authors...), item.Genres...), " ")
		return strings.Contains(strings.ToLower(haystack), strings.ToLower(query))
	})
	s.publicationFeed(w, r, "Search Results", items)
}

func (s *Server) authors(w http.ResponseWriter, r *http.Request) {
	values := uniqueValues(s.catalog.Items(""), func(item library.Publication) []string { return item.Authors })
	s.navigationFeed(w, r, "Authors", values, "author")
}

func (s *Server) genres(w http.ResponseWriter, r *http.Request) {
	values := uniqueValues(s.catalog.Items(""), func(item library.Publication) []string { return item.Genres })
	s.navigationFeed(w, r, "Genres", values, "genre")
}

func (s *Server) navigationFeed(w http.ResponseWriter, r *http.Request, title string, values []string, parameter string) {
	base := s.requestBase(r)
	navigation := make([]link, 0, len(values))
	for _, value := range values {
		target := base + "/opds/books?" + url.Values{parameter: []string{value}}.Encode()
		navigation = append(navigation, link{Href: target, Type: feedType, Rel: "subsection", Title: value})
	}
	s.writeFeed(w, feed{Metadata: metadata(title, len(values)), Links: feedLinks(base, r), Navigation: navigation})
}

func (s *Server) publicationFeed(w http.ResponseWriter, r *http.Request, title string, items []library.Publication) {
	base := s.requestBase(r)
	page, perPage, pageItems := paginate(items, r)
	publications := make([]publication, 0, len(pageItems))
	for _, item := range pageItems {
		publications = append(publications, encodePublication(base, item))
	}
	feedMetadata := metadata(title, len(items))
	feedMetadata["itemsPerPage"] = perPage
	feedMetadata["currentPage"] = page
	s.writeFeed(w, feed{
		Metadata:     feedMetadata,
		Links:        paginationLinks(base, r, page, perPage, len(items)),
		Publications: publications,
	})
}

func encodePublication(base string, item library.Publication) publication {
	publicationType := "http://schema.org/Book"
	if item.Kind == "ebook" {
		publicationType = "http://schema.org/EBook"
	} else if item.Kind == "audiobook" {
		publicationType = "http://schema.org/Audiobook"
	}
	meta := map[string]any{"@type": publicationType, "identifier": "urn:caxton:" + item.ID, "title": item.Title, "modified": item.Modified.UTC().Format(time.RFC3339)}
	if len(item.Authors) > 0 {
		authors := make([]map[string]string, 0, len(item.Authors))
		for _, name := range item.Authors {
			authors = append(authors, map[string]string{"name": name})
		}
		meta["author"] = authors
	}
	if item.Description != "" {
		meta["description"] = item.Description
	}
	if item.Language != "" {
		meta["language"] = item.Language
	}
	if item.Publisher != "" {
		meta["publisher"] = item.Publisher
	}
	if item.Published != nil {
		meta["published"] = item.Published.UTC().Format(time.RFC3339)
	}
	if item.Duration > 0 {
		meta["duration"] = item.Duration
	}
	if len(item.Genres) > 0 {
		meta["subject"] = item.Genres
	}
	if item.Series != "" {
		meta["belongsTo"] = map[string]any{"series": []map[string]string{{"name": item.Series}}}
	}
	links := []link{{
		Href: base + "/books/" + item.ID, Type: item.MediaType,
		Rel:        []string{"http://opds-spec.org/acquisition/open-access"},
		Properties: map[string]any{"fileSize": item.Size},
	}}
	var images []link
	if item.CoverType != "" {
		images = append(images, link{
			Href: base + "/covers/" + item.ID,
			Type: item.CoverType,
		})
	}
	return publication{Metadata: meta, Links: links, Images: images}
}

func feedLinks(base string, r *http.Request) []link {
	return []link{
		{Href: base + r.URL.RequestURI(), Type: feedType, Rel: "self"},
		{Href: base + "/opds", Type: feedType, Rel: "start"},
	}
}

func paginationLinks(base string, r *http.Request, page, perPage, total int) []link {
	links := feedLinks(base, r)
	if total <= perPage {
		return links
	}
	last := (total + perPage - 1) / perPage
	pageLink := func(target int, rel any) link {
		query := r.URL.Query()
		query.Set("page", strconv.Itoa(target))
		query.Set("per_page", strconv.Itoa(perPage))
		return link{Href: base + r.URL.Path + "?" + query.Encode(), Type: feedType, Rel: rel}
	}
	links = append(links, pageLink(1, "first"), pageLink(last, "last"))
	if page > 1 {
		links = append(links, pageLink(page-1, []string{"previous", "prev"}))
	}
	if page < last {
		links = append(links, pageLink(page+1, "next"))
	}
	return links
}

func paginate(items []library.Publication, r *http.Request) (int, int, []library.Publication) {
	page := positiveInt(r.URL.Query().Get("page"), 1)
	perPage := positiveInt(r.URL.Query().Get("per_page"), 50)
	if perPage > 100 {
		perPage = 100
	}
	start := (page - 1) * perPage
	if start >= len(items) {
		return page, perPage, nil
	}
	end := start + perPage
	if end > len(items) {
		end = len(items)
	}
	return page, perPage, items[start:end]
}

func positiveInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return fallback
	}
	return parsed
}

func filterItems(items []library.Publication, keep func(library.Publication) bool) []library.Publication {
	filtered := make([]library.Publication, 0, len(items))
	for _, item := range items {
		if keep(item) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func containsFold(values []string, wanted string) bool {
	for _, value := range values {
		if strings.EqualFold(value, wanted) {
			return true
		}
	}
	return false
}

func containsSubstringFold(values []string, wanted string) bool {
	wanted = strings.ToLower(wanted)
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), wanted) {
			return true
		}
	}
	return false
}

func uniqueValues(items []library.Publication, values func(library.Publication) []string) []string {
	seen := make(map[string]string)
	for _, item := range items {
		for _, value := range values(item) {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			key := strings.ToLower(value)
			if _, ok := seen[key]; !ok {
				seen[key] = value
			}
		}
	}
	result := make([]string, 0, len(seen))
	for _, value := range seen {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return strings.ToLower(result[i]) < strings.ToLower(result[j]) })
	return result
}

func metadata(title string, count int) map[string]any {
	return map[string]any{"title": title, "numberOfItems": count, "modified": time.Now().UTC().Format(time.RFC3339)}
}

func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	item, ok := s.catalog.Find(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(item.Path)
	if err != nil {
		http.Error(w, "book unavailable", http.StatusNotFound)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.Error(w, "book unavailable", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", item.MediaType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s", url.PathEscape(filepath.Base(item.Path))))
	http.ServeContent(w, r, filepath.Base(item.Path), info.ModTime(), f)
}

func (s *Server) cover(w http.ResponseWriter, r *http.Request) {
	data, mediaType, ok := s.catalog.Cover(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeContent(w, r, "cover", time.Time{}, bytes.NewReader(data))
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Server) requestBase(r *http.Request) string {
	if s.baseURL != "" {
		return s.baseURL
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]); forwarded == "http" || forwarded == "https" {
		scheme = forwarded
	}
	return scheme + "://" + r.Host
}

func (s *Server) writeFeed(w http.ResponseWriter, value feed) {
	w.Header().Set("Content-Type", feedType)
	w.Header().Set("Cache-Control", "no-cache")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return
	}
}
