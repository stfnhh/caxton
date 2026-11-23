package server

import (
	"encoding/xml"
	"net/http"
	"strconv"
	"strings"
	"time"

	"caxton/internal/library"
)

const atomType = "application/atom+xml;profile=opds-catalog"
const atomNavigationType = atomType + ";kind=navigation"
const atomAcquisitionType = atomType + ";kind=acquisition"
const openSearchType = "application/opensearchdescription+xml"

type atomLink struct {
	Href   string `xml:"href,attr"`
	Type   string `xml:"type,attr"`
	Rel    string `xml:"rel,attr"`
	Length int64  `xml:"length,attr,omitempty"`
}
type atomAuthor struct {
	Name string `xml:"name"`
}
type atomCategory struct {
	Term string `xml:"term,attr"`
}
type atomText struct {
	Type string `xml:"type,attr"`
	Text string `xml:",chardata"`
}
type atomEntry struct {
	ID         string         `xml:"id"`
	Title      string         `xml:"title"`
	Updated    string         `xml:"updated"`
	Authors    []atomAuthor   `xml:"author"`
	Summary    *atomText      `xml:"summary,omitempty"`
	Content    *atomText      `xml:"content,omitempty"`
	Categories []atomCategory `xml:"category"`
	Language   string         `xml:"http://purl.org/dc/terms/ language,omitempty"`
	Publisher  string         `xml:"http://purl.org/dc/terms/ publisher,omitempty"`
	Issued     string         `xml:"http://purl.org/dc/terms/ issued,omitempty"`
	Series     string         `xml:"http://schema.org/ isPartOf,omitempty"`
	Duration   string         `xml:"http://schema.org/ duration,omitempty"`
	Links      []atomLink     `xml:"link"`
}
type atomFeed struct {
	XMLName xml.Name    `xml:"http://www.w3.org/2005/Atom feed"`
	ID      string      `xml:"id"`
	Title   string      `xml:"title"`
	Updated string      `xml:"updated"`
	Author  atomAuthor  `xml:"author"`
	Links   []atomLink  `xml:"link"`
	Entries []atomEntry `xml:"entry"`
}

func usesAtom(r *http.Request) bool {
	return r.URL.Path == opdsV1Root || strings.HasPrefix(r.URL.Path, opdsV1Root+"/")
}

func atomLinks(links []catalogLink) []atomLink {
	result := make([]atomLink, 0, len(links))
	for _, item := range links {
		if item.Templated {
			continue
		}
		href := item.Href
		var relations []string
		switch rel := item.Rel.(type) {
		case string:
			relations = []string{rel}
		case []string:
			relations = rel
		}
		for _, rel := range relations {
			if rel == "previous" {
				continue
			}
			result = append(result, atomLink{Href: href, Type: item.Type, Rel: rel})
		}
	}
	return result
}

func (s *Server) newAtomFeed(r *http.Request, title string, links []catalogLink) atomFeed {
	base := s.requestBase(r)
	converted := atomLinks(links)
	converted = append(converted, atomLink{Href: base + opdsV1Root + "/search.xml", Type: openSearchType, Rel: "search"})
	return atomFeed{ID: base + r.URL.RequestURI(), Title: title, Updated: time.Now().UTC().Format(time.RFC3339), Author: atomAuthor{Name: "Caxton"}, Links: converted}
}

func (s *Server) writeAtomNavigation(w http.ResponseWriter, r *http.Request, value catalogFeed) {
	f := s.newAtomFeed(r, value.Metadata["title"].(string), value.Links)
	for _, item := range value.Navigation {
		links := atomLinks([]catalogLink{item})
		f.Entries = append(f.Entries, atomEntry{
			ID:      links[0].Href,
			Title:   item.Title,
			Updated: f.Updated,
			Content: &atomText{Type: "text", Text: "Browse " + item.Title},
			Links:   links,
		})
	}
	writeXML(w, atomNavigationType, f)
}

func (s *Server) writeAtomPublications(w http.ResponseWriter, r *http.Request, title string, items []library.Publication, page, perPage, total int) {
	base := s.requestBase(r)
	f := s.newAtomFeed(r, title, paginationLinks(base, r, page, perPage, total))
	for _, item := range items {
		f.Entries = append(f.Entries, encodeAtomPublication(base, item))
	}
	writeXML(w, atomAcquisitionType, f)
}

func encodeAtomPublication(base string, item library.Publication) atomEntry {
	entry := atomEntry{ID: "urn:caxton:" + item.ID, Title: item.Title, Updated: item.Modified.UTC().Format(time.RFC3339), Language: item.Language, Publisher: item.Publisher, Series: item.Series}
	if item.Duration > 0 {
		entry.Duration = "PT" + strconv.FormatFloat(item.Duration, 'f', -1, 64) + "S"
	}
	for _, name := range item.Authors {
		entry.Authors = append(entry.Authors, atomAuthor{Name: name})
	}
	for _, genre := range item.Genres {
		entry.Categories = append(entry.Categories, atomCategory{Term: genre})
	}
	if item.Description != "" {
		entry.Summary = &atomText{Type: "text", Text: item.Description}
	}
	if item.Published != nil {
		entry.Issued = item.Published.UTC().Format(time.RFC3339)
	}
	entry.Links = []atomLink{{Href: base + "/books/" + item.ID, Type: item.MediaType, Rel: "http://opds-spec.org/acquisition/open-access", Length: item.Size}}
	if item.CoverType != "" {
		entry.Links = append(entry.Links, atomLink{Href: base + "/covers/" + item.ID, Type: item.CoverType, Rel: "http://opds-spec.org/image"})
	}
	return entry
}

func (s *Server) openSearch(w http.ResponseWriter, r *http.Request) {
	description := struct {
		XMLName     xml.Name `xml:"http://a9.com/-/spec/opensearch/1.1/ OpenSearchDescription"`
		ShortName   string   `xml:"ShortName"`
		Description string   `xml:"Description"`
		URL         struct {
			Type     string `xml:"type,attr"`
			Template string `xml:"template,attr"`
		} `xml:"Url"`
	}{ShortName: "Caxton", Description: "Search books by title, author, series, or genre"}
	description.URL.Type = atomAcquisitionType
	description.URL.Template = s.requestBase(r) + opdsV1Root + "/search?query={searchTerms}"
	writeXML(w, openSearchType, description)
}

func writeXML(w http.ResponseWriter, mediaType string, value any) {
	data, err := xml.Marshal(value)
	if err != nil {
		http.Error(w, "cannot encode catalog", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(append([]byte(xml.Header), data...))
}
