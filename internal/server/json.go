package server

import (
	"encoding/json"
	"net/http"
	"time"

	"caxton/internal/library"
)

const jsonFeedType = "application/opds+json"
const jsonPublicationType = "application/opds-publication+json"

type jsonLink struct {
	Href       string         `json:"href"`
	Type       string         `json:"type,omitempty"`
	Rel        any            `json:"rel,omitempty"`
	Title      string         `json:"title,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
	Templated  bool           `json:"templated,omitempty"`
}

type jsonFeed struct {
	Metadata     map[string]any     `json:"metadata"`
	Links        []jsonLink         `json:"links"`
	Navigation   *[]jsonLink        `json:"navigation,omitempty"`
	Publications *[]jsonPublication `json:"publications,omitempty"`
}

type jsonPublication struct {
	Metadata map[string]any `json:"metadata"`
	Links    []jsonLink     `json:"links"`
	Images   []jsonLink     `json:"images,omitempty"`
}

func writeJSONNavigation(w http.ResponseWriter, value catalogFeed) {
	navigation := jsonLinks(value.Navigation)
	writeJSON(w, jsonFeed{
		Metadata:   value.Metadata,
		Links:      jsonLinks(value.Links),
		Navigation: &navigation,
	})
}

func writeJSONPublications(w http.ResponseWriter, base string, metadata map[string]any, links []catalogLink, items []library.Publication) {
	publications := make([]jsonPublication, 0, len(items))
	for _, item := range items {
		publications = append(publications, encodeJSONPublication(base, item))
	}
	writeJSON(w, jsonFeed{
		Metadata:     metadata,
		Links:        jsonLinks(links),
		Publications: &publications,
	})
}

func (s *Server) jsonPublication(w http.ResponseWriter, r *http.Request) {
	item, ok := s.catalog.Find(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSONValue(w, jsonPublicationType, encodeJSONPublication(s.requestBase(r), item))
}

func jsonLinks(links []catalogLink) []jsonLink {
	result := make([]jsonLink, 0, len(links))
	for _, item := range links {
		result = append(result, jsonLink{
			Href:       item.Href,
			Type:       item.Type,
			Rel:        item.Rel,
			Title:      item.Title,
			Properties: item.Properties,
			Templated:  item.Templated,
		})
	}
	return result
}

func encodeJSONPublication(base string, item library.Publication) jsonPublication {
	publicationType := "http://schema.org/Book"
	if item.Kind == "ebook" {
		publicationType = "http://schema.org/EBook"
	} else if item.Kind == "audiobook" {
		publicationType = "http://schema.org/Audiobook"
	}
	metadata := map[string]any{
		"@type":      publicationType,
		"identifier": "urn:caxton:" + item.ID,
		"title":      item.Title,
		"modified":   item.Modified.UTC().Format(time.RFC3339),
	}
	if len(item.Authors) > 0 {
		authors := make([]map[string]string, 0, len(item.Authors))
		for _, name := range item.Authors {
			authors = append(authors, map[string]string{"name": name})
		}
		metadata["author"] = authors
	}
	if item.Description != "" {
		metadata["description"] = item.Description
	}
	if item.Language != "" {
		metadata["language"] = item.Language
	}
	if item.Publisher != "" {
		metadata["publisher"] = item.Publisher
	}
	if item.Published != nil {
		metadata["published"] = item.Published.UTC().Format(time.RFC3339)
	}
	if item.Duration > 0 {
		metadata["duration"] = item.Duration
	}
	if len(item.Genres) > 0 {
		metadata["subject"] = item.Genres
	}
	if item.Series != "" {
		metadata["belongsTo"] = map[string]any{"series": []map[string]string{{"name": item.Series}}}
	}

	links := []jsonLink{{
		Href: base + opdsV2Root + "/publications/" + item.ID,
		Type: jsonPublicationType,
		Rel:  "self",
	}, {
		Href: base + "/books/" + item.ID,
		Type: item.MediaType,
		Rel:  []string{"http://opds-spec.org/acquisition/open-access"},
		Properties: map[string]any{
			"fileSize": item.Size,
		},
	}}
	var images []jsonLink
	if item.CoverType != "" {
		images = append(images, jsonLink{
			Href: base + "/covers/" + item.ID,
			Type: item.CoverType,
		})
	}
	return jsonPublication{Metadata: metadata, Links: links, Images: images}
}

func writeJSON(w http.ResponseWriter, value jsonFeed) {
	writeJSONValue(w, jsonFeedType, value)
}

func writeJSONValue(w http.ResponseWriter, mediaType string, value any) {
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Cache-Control", "no-cache")
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
}
