package library

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Publication struct {
	ID          string
	Title       string
	Authors     []string
	Genres      []string
	Series      string
	Description string
	Language    string
	Publisher   string
	Published   *time.Time
	Added       time.Time
	Modified    time.Time
	Duration    float64
	MediaType   string
	Path        string
	Size        int64
	Kind        string
	CoverType   string
	CoverData   []byte
}

type Catalog struct {
	dir    string
	covers string
	db     *sql.DB
	mu     sync.RWMutex
	items  []Publication
}

func NewCatalog(dir, databasePath, coverDir string) (*Catalog, error) {
	db, err := openStore(databasePath)
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		db.Close()
		return nil, err
	}
	coverAbs, err := filepath.Abs(coverDir)
	if err != nil {
		db.Close()
		return nil, err
	}
	if err := os.MkdirAll(coverAbs, 0o755); err != nil {
		db.Close()
		return nil, fmt.Errorf("create cover cache: %w", err)
	}
	return &Catalog{dir: abs, covers: coverAbs, db: db}, nil
}

func (c *Catalog) Close() error { return c.db.Close() }

func (c *Catalog) Items(kind string) []Publication {
	c.mu.RLock()
	defer c.mu.RUnlock()
	items := make([]Publication, 0, len(c.items))
	for _, item := range c.items {
		if kind == "" || item.Kind == kind {
			items = append(items, item)
		}
	}
	return items
}

func (c *Catalog) Find(id string) (Publication, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, item := range c.items {
		if item.ID == id {
			return item, true
		}
	}
	return Publication{}, false
}

func (c *Catalog) Cover(id string) ([]byte, string, bool) {
	var mediaType string
	if err := c.db.QueryRow(`SELECT cover_type FROM publications WHERE id=? AND cover_type<>''`, id).Scan(&mediaType); err != nil {
		return nil, "", false
	}
	data, err := os.ReadFile(filepath.Join(c.covers, id))
	return data, mediaType, err == nil && len(data) > 0
}

func (c *Catalog) Scan() error {
	var items []Publication
	err := filepath.WalkDir(c.dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".epub" && ext != ".m4b" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		item, cached, err := loadCached(c.db, path, info, c.covers)
		if err != nil {
			return err
		}
		if !cached {
			item, err = readMetadata(path, info)
			item.Added = firstSeen(c.db, path, time.Now().UTC())
		}
		if err != nil {
			log.Printf("skip %s: %v", path, err)
			return nil
		}
		if !cached {
			if err := saveCached(c.db, item, c.covers); err != nil {
				return err
			}
		}
		items = append(items, item)
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("library directory %q does not exist", c.dir)
	}
	if err != nil {
		return err
	}
	if err := removeMissing(c.db, c.dir, c.covers, items); err != nil {
		return err
	}
	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(items[i].Title) < strings.ToLower(items[j].Title)
	})
	c.mu.Lock()
	c.items = items
	c.mu.Unlock()
	return nil
}

func (c *Catalog) Run(ctx context.Context, interval time.Duration, onError func(error)) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.Scan(); err != nil && onError != nil {
				onError(err)
			}
		}
	}
}

func stableID(path string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(path)))
	return hex.EncodeToString(sum[:12])
}
