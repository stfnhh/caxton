package server

import (
	"bytes"
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

const (
	opdsV1Root = "/opds/v1"
	opdsV2Root = "/opds/v2"
	sortNewRel = "http://opds-spec.org/sort/new"
)

type Server struct {
	catalog *library.Catalog
	baseURL string
	mux     *http.ServeMux
}

func New(catalog *library.Catalog, baseURL string) http.Handler {
	s := &Server{catalog: catalog, baseURL: strings.TrimRight(baseURL, "/"), mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /", s.root)
	s.registerCatalog(opdsV1Root)
	s.registerCatalog(opdsV2Root)
	s.mux.HandleFunc("GET "+opdsV1Root+"/search.xml", s.openSearch)
	s.mux.HandleFunc("GET "+opdsV2Root+"/publications/{id}", s.jsonPublication)
	s.mux.HandleFunc("GET /opds", redirectTo(opdsV2Root, http.StatusTemporaryRedirect))
	s.mux.HandleFunc("GET /books/{id}", s.download)
	s.mux.HandleFunc("GET /covers/{id}", s.cover)
	s.mux.HandleFunc("GET /healthz", s.health)
	return s.mux
}

func (s *Server) registerCatalog(root string) {
	s.mux.HandleFunc("GET "+root, s.catalogFeed)
	s.mux.HandleFunc("GET "+root+"/books", s.allPublications)
	s.mux.HandleFunc("GET "+root+"/recent", s.recent)
	s.mux.HandleFunc("GET "+root+"/authors", s.authors)
	s.mux.HandleFunc("GET "+root+"/genres", s.genres)
	s.mux.HandleFunc("GET "+root+"/search", s.search)
	s.mux.HandleFunc("GET "+root+"/ebooks", s.ebooks)
	s.mux.HandleFunc("GET "+root+"/audiobooks", s.audiobooks)
}

func redirectTo(target string, status int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		location := target
		if r.URL.RawQuery != "" {
			location += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, location, status)
	}
}

type catalogLink struct {
	Href       string
	Type       string
	Rel        any
	Title      string
	Properties map[string]any
	Templated  bool
}

type catalogFeed struct {
	Metadata   map[string]any
	Links      []catalogLink
	Navigation []catalogLink
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
	root := catalogRoot(r)
	navigationType := navigationFeedType(r)
	publicationType := publicationFeedType(r)
	items := s.catalog.Items("")
	s.writeNavigationFeed(w, r, catalogFeed{
		Metadata: metadata("Caxton", len(items)),
		Links: []catalogLink{
			{Href: base + root, Type: navigationType, Rel: "self"},
			{Href: base + root, Type: navigationType, Rel: "start"},
			{Href: base + root + "/search{?query,title,author}", Type: publicationType, Rel: "search", Templated: true},
		},
		Navigation: []catalogLink{
			{Href: base + root + "/recent", Type: publicationType, Rel: sortNewRel, Title: "Recently Added"},
			{Href: base + root + "/books", Type: publicationType, Rel: "subsection", Title: "All Books"},
			{Href: base + root + "/authors", Type: navigationType, Rel: "subsection", Title: "Authors"},
			{Href: base + root + "/genres", Type: navigationType, Rel: "subsection", Title: "Genres"},
			{Href: base + root + "/ebooks", Type: publicationType, Rel: "subsection", Title: "eBooks"},
			{Href: base + root + "/audiobooks", Type: publicationType, Rel: "subsection", Title: "Audiobooks"},
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
	root := catalogRoot(r)
	mediaType := publicationFeedType(r)
	navigation := make([]catalogLink, 0, len(values))
	for _, value := range values {
		target := base + root + "/books?" + url.Values{parameter: []string{value}}.Encode()
		navigation = append(navigation, catalogLink{Href: target, Type: mediaType, Rel: "subsection", Title: value})
	}
	s.writeNavigationFeed(w, r, catalogFeed{Metadata: metadata(title, len(values)), Links: feedLinks(base, r), Navigation: navigation})
}

func (s *Server) publicationFeed(w http.ResponseWriter, r *http.Request, title string, items []library.Publication) {
	base := s.requestBase(r)
	page, perPage, pageItems := paginate(items, r)
	if usesAtom(r) {
		s.writeAtomPublications(w, r, title, pageItems, page, perPage, len(items))
		return
	}
	feedMetadata := metadata(title, len(items))
	feedMetadata["itemsPerPage"] = perPage
	feedMetadata["currentPage"] = page
	writeJSONPublications(w, base, feedMetadata, paginationLinks(base, r, page, perPage, len(items)), pageItems)
}

func feedLinks(base string, r *http.Request) []catalogLink {
	return []catalogLink{
		{Href: base + r.URL.RequestURI(), Type: currentFeedType(r), Rel: "self"},
		{Href: base + catalogRoot(r), Type: navigationFeedType(r), Rel: "start"},
	}
}

func catalogRoot(r *http.Request) string {
	if usesAtom(r) {
		return opdsV1Root
	}
	return opdsV2Root
}

func currentFeedType(r *http.Request) string {
	if r.URL.Path == opdsV1Root || r.URL.Path == opdsV2Root || strings.HasSuffix(r.URL.Path, "/authors") || strings.HasSuffix(r.URL.Path, "/genres") {
		return navigationFeedType(r)
	}
	return publicationFeedType(r)
}

func navigationFeedType(r *http.Request) string {
	if usesAtom(r) {
		return atomNavigationType
	}
	return jsonFeedType
}

func publicationFeedType(r *http.Request) string {
	if usesAtom(r) {
		return atomAcquisitionType
	}
	return jsonFeedType
}

func paginationLinks(base string, r *http.Request, page, perPage, total int) []catalogLink {
	links := feedLinks(base, r)
	if total <= perPage {
		return links
	}
	last := (total + perPage - 1) / perPage
	pageLink := func(target int, rel any) catalogLink {
		query := r.URL.Query()
		query.Set("page", strconv.Itoa(target))
		query.Set("per_page", strconv.Itoa(perPage))
		return catalogLink{Href: base + r.URL.Path + "?" + query.Encode(), Type: publicationFeedType(r), Rel: rel}
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

func (s *Server) writeNavigationFeed(w http.ResponseWriter, r *http.Request, value catalogFeed) {
	if usesAtom(r) {
		s.writeAtomNavigation(w, r, value)
		return
	}
	writeJSONNavigation(w, value)
}
