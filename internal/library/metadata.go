package library

import (
	"archive/zip"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dhowden/tag"
)

func readMetadata(path string, info fs.FileInfo) (Publication, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".epub" {
		return readEPUB(path, info)
	}
	return readM4B(path, info)
}

type containerXML struct {
	Rootfiles []struct {
		FullPath string `xml:"full-path,attr"`
	} `xml:"rootfiles>rootfile"`
}

type packageXML struct {
	Metadata struct {
		Titles      []string `xml:"title"`
		Creators    []string `xml:"creator"`
		Description string   `xml:"description"`
		Language    string   `xml:"language"`
		Publisher   string   `xml:"publisher"`
		Subjects    []string `xml:"subject"`
		Dates       []string `xml:"date"`
		Meta        []struct {
			Name     string `xml:"name,attr"`
			Content  string `xml:"content,attr"`
			Property string `xml:"property,attr"`
			Value    string `xml:",chardata"`
		} `xml:"meta"`
	} `xml:"metadata"`
	Manifest struct {
		Items []struct {
			ID         string `xml:"id,attr"`
			Href       string `xml:"href,attr"`
			MediaType  string `xml:"media-type,attr"`
			Properties string `xml:"properties,attr"`
		} `xml:"item"`
	} `xml:"manifest"`
}

func readEPUB(path string, info fs.FileInfo) (Publication, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return Publication{}, err
	}
	defer zr.Close()
	containerData, err := zipFile(zr.File, "META-INF/container.xml")
	if err != nil {
		return Publication{}, err
	}
	var container containerXML
	if err := xml.Unmarshal(containerData, &container); err != nil || len(container.Rootfiles) == 0 {
		return Publication{}, fmt.Errorf("invalid EPUB container")
	}
	packageData, err := zipFile(zr.File, container.Rootfiles[0].FullPath)
	if err != nil {
		return Publication{}, err
	}
	var pkg packageXML
	if err := xml.Unmarshal(packageData, &pkg); err != nil {
		return Publication{}, err
	}
	title := first(pkg.Metadata.Titles)
	if title == "" {
		title = filenameTitle(path)
	}
	item := basePublication(path, info, title, "application/epub+zip", "ebook")
	item.Authors = cleanStrings(pkg.Metadata.Creators)
	item.Description = strings.TrimSpace(pkg.Metadata.Description)
	item.Language = strings.TrimSpace(pkg.Metadata.Language)
	item.Publisher = strings.TrimSpace(pkg.Metadata.Publisher)
	item.Genres = cleanStrings(pkg.Metadata.Subjects)
	item.Published = parseDate(first(pkg.Metadata.Dates))
	coverID := ""
	for _, meta := range pkg.Metadata.Meta {
		if strings.EqualFold(meta.Name, "cover") {
			coverID = meta.Content
		}
		if strings.EqualFold(meta.Name, "calibre:series") {
			item.Series = strings.TrimSpace(meta.Content)
		}
		if meta.Property == "belongs-to-collection" && item.Series == "" {
			item.Series = strings.TrimSpace(meta.Value)
		}
	}
	for _, manifestItem := range pkg.Manifest.Items {
		if manifestItem.ID != coverID && !hasToken(manifestItem.Properties, "cover-image") {
			continue
		}
		coverPath := filepath.ToSlash(filepath.Join(filepath.Dir(container.Rootfiles[0].FullPath), manifestItem.Href))
		if data, err := zipFile(zr.File, coverPath); err == nil {
			item.CoverData = data
			item.CoverType = manifestItem.MediaType
		}
		break
	}
	return item, nil
}

func readM4B(path string, info fs.FileInfo) (Publication, error) {
	f, err := os.Open(path)
	if err != nil {
		return Publication{}, err
	}
	defer f.Close()
	item := basePublication(path, info, filenameTitle(path), "audio/mp4", "audiobook")
	item.Duration = mp4Duration(f, info.Size())
	metadata, tagErr := tag.ReadFrom(f)
	if tagErr != nil {
		// An otherwise valid, untagged M4B should still appear in the catalog.
		return item, nil
	}
	if title := strings.TrimSpace(metadata.Title()); title != "" {
		item.Title = title
	}
	artist := strings.TrimSpace(metadata.Artist())
	if artist == "" {
		artist = strings.TrimSpace(metadata.AlbumArtist())
	}
	if artist != "" {
		item.Authors = []string{artist}
	}
	item.Series = strings.TrimSpace(metadata.Album())
	if genre := strings.TrimSpace(metadata.Genre()); genre != "" {
		item.Genres = []string{genre}
	}
	item.Description = strings.TrimSpace(metadata.Comment())
	item.Published = parseDate(fmt.Sprint(metadata.Year()))
	if picture := metadata.Picture(); picture != nil {
		item.CoverData = picture.Data
		item.CoverType = picture.MIMEType
	}
	return item, nil
}

func mp4Duration(f *os.File, fileSize int64) float64 {
	moovStart, moovEnd, ok := findMP4Atom(f, 0, fileSize, "moov")
	if !ok {
		return 0
	}
	mvhdStart, mvhdEnd, ok := findMP4Atom(f, moovStart, moovEnd, "mvhd")
	if !ok {
		return 0
	}
	buf := make([]byte, 32)
	n := int64(len(buf))
	if mvhdEnd-mvhdStart < n {
		n = mvhdEnd - mvhdStart
	}
	if n < 20 {
		return 0
	}
	if _, err := f.ReadAt(buf[:n], mvhdStart); err != nil {
		return 0
	}
	if buf[0] == 1 {
		if n < 32 {
			return 0
		}
		timescale := binary.BigEndian.Uint32(buf[20:24])
		duration := binary.BigEndian.Uint64(buf[24:32])
		if timescale > 0 {
			return float64(duration) / float64(timescale)
		}
		return 0
	}
	timescale := binary.BigEndian.Uint32(buf[12:16])
	duration := binary.BigEndian.Uint32(buf[16:20])
	if timescale > 0 {
		return float64(duration) / float64(timescale)
	}
	return 0
}

// findMP4Atom returns the payload boundaries of an atom within [start,end).
func findMP4Atom(f *os.File, start, end int64, wanted string) (int64, int64, bool) {
	header := make([]byte, 16)
	for pos := start; pos+8 <= end; {
		if _, err := f.ReadAt(header[:8], pos); err != nil {
			return 0, 0, false
		}
		size := int64(binary.BigEndian.Uint32(header[:4]))
		headerSize := int64(8)
		if size == 1 {
			if _, err := f.ReadAt(header[8:16], pos+8); err != nil {
				return 0, 0, false
			}
			size = int64(binary.BigEndian.Uint64(header[8:16]))
			headerSize = 16
		} else if size == 0 {
			size = end - pos
		}
		if size < headerSize || pos+size > end {
			return 0, 0, false
		}
		if string(header[4:8]) == wanted {
			return pos + headerSize, pos + size, true
		}
		pos += size
	}
	return 0, 0, false
}

func basePublication(path string, info fs.FileInfo, title, mediaType, kind string) Publication {
	return Publication{ID: stableID(path), Title: title, Modified: info.ModTime(), MediaType: mediaType, Path: path, Size: info.Size(), Kind: kind}
}

func zipFile(files []*zip.File, name string) ([]byte, error) {
	for _, file := range files {
		if filepath.ToSlash(file.Name) != filepath.ToSlash(name) {
			continue
		}
		r, err := file.Open()
		if err != nil {
			return nil, err
		}
		defer r.Close()
		return io.ReadAll(io.LimitReader(r, 8<<20))
	}
	return nil, fmt.Errorf("EPUB entry %q not found", name)
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}

func cleanStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func hasToken(value, wanted string) bool {
	for _, token := range strings.Fields(value) {
		if token == wanted {
			return true
		}
	}
	return false
}

func filenameTitle(path string) string {
	return strings.TrimSpace(strings.ReplaceAll(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), "_", " "))
}

func parseDate(value string) *time.Time {
	value = strings.TrimSpace(value)
	for _, layout := range []string{time.RFC3339, "2006-01-02", "2006"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			if parsed.Year() < 1000 {
				return nil
			}
			return &parsed
		}
	}
	return nil
}
