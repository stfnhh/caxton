package library

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

func openStore(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	const schema = `
		CREATE TABLE IF NOT EXISTS publications (
		    path               TEXT PRIMARY KEY,
		    id                 TEXT NOT NULL,
		    title              TEXT NOT NULL,
		    authors            TEXT NOT NULL,
		    genres             TEXT NOT NULL DEFAULT '[]',
		    series             TEXT NOT NULL DEFAULT '',
		    description        TEXT NOT NULL,
		    language           TEXT NOT NULL,
		    publisher          TEXT NOT NULL,
		    published          TEXT,
		    added_at           TEXT NOT NULL DEFAULT '',
		    modified_ns        INTEGER NOT NULL,
		    duration           REAL NOT NULL,
		    media_type         TEXT NOT NULL,
		    size               INTEGER NOT NULL,
		    kind               TEXT NOT NULL,
		    cover_type         TEXT NOT NULL DEFAULT '',
		    extraction_version INTEGER NOT NULL DEFAULT 0
		);

		CREATE INDEX IF NOT EXISTS publications_path_idx
		    ON publications (path);
	`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize SQLite database: %w", err)
	}
	for _, migration := range []string{
		`ALTER TABLE publications ADD COLUMN cover_type TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE publications ADD COLUMN extraction_version INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE publications ADD COLUMN genres TEXT NOT NULL DEFAULT '[]'`,
		`ALTER TABLE publications ADD COLUMN series TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE publications ADD COLUMN added_at TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := db.Exec(migration); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
			db.Close()
			return nil, fmt.Errorf("migrate SQLite database: %w", err)
		}
	}
	return db, nil
}

func loadCached(db *sql.DB, path string, info fs.FileInfo, coverDir string) (Publication, bool, error) {
	var item Publication
	var authorsJSON, genresJSON, published, added string

	const query = `
		SELECT
		    id,
		    title,
		    authors,
		    genres,
		    series,
		    description,
		    language,
		    publisher,
		    COALESCE(published, ''),
		    added_at,
		    modified_ns,
		    duration,
		    media_type,
		    path,
		    size,
		    kind,
		    cover_type
		FROM publications
		WHERE path = ?
		    AND size = ?
		    AND modified_ns = ?
		    AND extraction_version = 2
	`

	err := db.QueryRow(query, path, info.Size(), info.ModTime().UnixNano()).Scan(
		&item.ID,
		&item.Title,
		&authorsJSON,
		&genresJSON,
		&item.Series,
		&item.Description,
		&item.Language,
		&item.Publisher,
		&published,
		&added,
		new(int64),
		&item.Duration,
		&item.MediaType,
		&item.Path,
		&item.Size,
		&item.Kind,
		&item.CoverType,
	)
	if err == sql.ErrNoRows {
		return Publication{}, false, nil
	}
	if err != nil {
		return Publication{}, false, err
	}
	item.Modified = info.ModTime()
	if err := json.Unmarshal([]byte(authorsJSON), &item.Authors); err != nil {
		return Publication{}, false, err
	}
	if err := json.Unmarshal([]byte(genresJSON), &item.Genres); err != nil {
		return Publication{}, false, err
	}
	item.Published = parseDate(published)
	item.Added, _ = time.Parse(time.RFC3339Nano, added)
	if item.CoverType != "" {
		if _, err := os.Stat(filepath.Join(coverDir, item.ID)); err != nil {
			return Publication{}, false, nil
		}
	}
	return item, true, nil
}

func saveCached(db *sql.DB, item Publication, coverDir string) error {
	authors, err := json.Marshal(item.Authors)
	if err != nil {
		return err
	}
	genres, err := json.Marshal(item.Genres)
	if err != nil {
		return err
	}
	published := ""
	if item.Published != nil {
		published = item.Published.Format(time.RFC3339)
	}

	if err := saveCover(item, coverDir); err != nil {
		return err
	}

	const query = `
		INSERT INTO publications (
		    path,
		    id,
		    title,
		    authors,
		    genres,
		    series,
		    description,
		    language,
		    publisher,
		    published,
		    added_at,
		    modified_ns,
		    duration,
		    media_type,
		    size,
		    kind,
		    cover_type,
		    extraction_version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 2)
		ON CONFLICT (path) DO UPDATE SET
		    id                 = excluded.id,
		    title              = excluded.title,
		    authors            = excluded.authors,
		    genres             = excluded.genres,
		    series             = excluded.series,
		    description        = excluded.description,
		    language           = excluded.language,
		    publisher          = excluded.publisher,
		    published          = excluded.published,
		    added_at           = CASE
		        WHEN publications.added_at = '' THEN excluded.added_at
		        ELSE publications.added_at
		    END,
		    modified_ns        = excluded.modified_ns,
		    duration           = excluded.duration,
		    media_type         = excluded.media_type,
		    size               = excluded.size,
		    kind               = excluded.kind,
		    cover_type         = excluded.cover_type,
		    extraction_version = excluded.extraction_version
	`

	_, err = db.Exec(
		query,
		item.Path,
		item.ID,
		item.Title,
		string(authors),
		string(genres),
		item.Series,
		item.Description,
		item.Language,
		item.Publisher,
		published,
		item.Added.Format(time.RFC3339Nano),
		item.Modified.UnixNano(),
		item.Duration,
		item.MediaType,
		item.Size,
		item.Kind,
		item.CoverType,
	)
	return err
}

func saveCover(item Publication, coverDir string) error {
	coverPath := filepath.Join(coverDir, item.ID)
	if len(item.CoverData) == 0 {
		_ = os.Remove(coverPath)
		return nil
	}

	tmp, err := os.CreateTemp(coverDir, ".cover-*")
	if err != nil {
		return fmt.Errorf("create temporary cover: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("set cover permissions: %w", err)
	}
	if _, err := tmp.Write(item.CoverData); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write cover: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close cover: %w", err)
	}
	if err := os.Rename(tmpName, coverPath); err != nil {
		return fmt.Errorf("cache cover: %w", err)
	}
	return nil
}

func firstSeen(db *sql.DB, path string, fallback time.Time) time.Time {
	var value string
	const query = `
		SELECT added_at
		FROM publications
		WHERE path = ?
	`
	if err := db.QueryRow(query, path).Scan(&value); err == nil {
		if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil && !parsed.IsZero() {
			return parsed
		}
	}
	return fallback
}

func removeMissing(db *sql.DB, libraryDir, coverDir string, items []Publication) error {
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		seen[item.Path] = true
	}
	const query = `
		SELECT
		    path,
		    id
		FROM publications
	`
	rows, err := db.Query(query)
	if err != nil {
		return err
	}
	type staleItem struct{ path, id string }
	var stale []staleItem
	for rows.Next() {
		var path string
		var id string
		if err := rows.Scan(&path, &id); err != nil {
			rows.Close()
			return err
		}
		rel, err := filepath.Rel(libraryDir, path)
		if err == nil && rel != ".." && !filepath.IsAbs(rel) && !seen[path] {
			stale = append(stale, staleItem{path: path, id: id})
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range stale {
		if _, err := db.Exec(`DELETE FROM publications WHERE path = ?`, item.path); err != nil {
			return err
		}
		_ = os.Remove(filepath.Join(coverDir, item.id))
	}
	return nil
}
