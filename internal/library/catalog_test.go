package library

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestCatalogCachesEPUBMetadata(t *testing.T) {
	dir := t.TempDir()
	book := filepath.Join(dir, "book.epub")
	writeTestEPUB(t, book, "The Test Book", "Ada Author")
	catalog, err := NewCatalog(dir, filepath.Join(t.TempDir(), "catalog.db"), filepath.Join(t.TempDir(), "covers"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	if err := catalog.Scan(); err != nil {
		t.Fatal(err)
	}
	items := catalog.Items("ebook")
	if len(items) != 1 || items[0].Title != "The Test Book" || items[0].Authors[0] != "Ada Author" {
		t.Fatalf("unexpected items: %#v", items)
	}
	if len(items[0].Genres) != 1 || items[0].Genres[0] != "Science Fiction" || items[0].Series != "Test Series" || items[0].Added.IsZero() {
		t.Fatalf("missing extended metadata: %#v", items[0])
	}
	if data, mediaType, ok := catalog.Cover(items[0].ID); !ok || mediaType != "image/jpeg" || string(data) != "fake-jpeg" {
		t.Fatalf("unexpected cover: type=%q ok=%v data=%q", mediaType, ok, data)
	}
	// A corrupt file with an unchanged size and timestamp still loads from SQLite.
	info, _ := os.Stat(book)
	data := make([]byte, info.Size())
	if err := os.WriteFile(book, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(book, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := catalog.Scan(); err != nil {
		t.Fatal(err)
	}
	if got := catalog.Items("ebook"); len(got) != 1 || got[0].Title != "The Test Book" {
		t.Fatalf("cache miss: %#v", got)
	}
}

func TestParseDateRejectsInvalidAncientYears(t *testing.T) {
	for _, value := range []string{"", "0", "0001-01-01"} {
		if got := parseDate(value); got != nil {
			t.Errorf("parseDate(%q) = %v, want nil", value, got)
		}
	}
	if got := parseDate("2021"); got == nil || got.Year() != 2021 {
		t.Fatalf("parseDate(2021) = %v", got)
	}
}

func writeTestEPUB(t *testing.T, path, title, author string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	files := map[string]string{
		"META-INF/container.xml": `<?xml version="1.0"?><container><rootfiles><rootfile full-path="content.opf"/></rootfiles></container>`,
		"content.opf":            `<?xml version="1.0"?><package xmlns:dc="http://purl.org/dc/elements/1.1/"><metadata><dc:title>` + title + `</dc:title><dc:creator>` + author + `</dc:creator><dc:language>en</dc:language><dc:subject>Science Fiction</dc:subject><meta name="calibre:series" content="Test Series"/></metadata><manifest><item id="cover" href="cover.jpg" media-type="image/jpeg" properties="cover-image"/></manifest></package>`,
		"cover.jpg":              "fake-jpeg",
	}
	for name, body := range files {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
