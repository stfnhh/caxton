<p align="center">
  <img src="docs/caxton.png" alt="Caxton" width="400">
</p>

Caxton is a small, read-only OPDS 2.0 server for EPUB ebooks and M4B audiobooks. It scans a directory every five minutes by default, stores extracted metadata in SQLite, and caches embedded cover images as ordinary files. Books are parsed only when their path, size, or modification time changes.

## Run

```sh
go run ./cmd/caxton -library /path/to/books
```

Open `http://localhost:8080/opds` in an OPDS 2.0 client.

Options can be flags or environment variables:

| Flag | Environment | Default |
| --- | --- | --- |
| `-library` | `CAXTON_LIBRARY` | `./library` |
| `-database` | `CAXTON_DATABASE` | `./caxton.db` |
| `-cover-cache` | `CAXTON_COVER_CACHE` | `./covers` |
| `-listen` | `CAXTON_LISTEN` | `:8080` |
| `-scan-interval` | `CAXTON_SCAN_INTERVAL` | `5m` |
| `-base-url` | `CAXTON_BASE_URL` | inferred from request |

Feeds:

- `/opds` — navigation catalog
- `/opds/books` — all publications, optionally filtered by `author` or `genre`
- `/opds/recent` — publications ordered by first discovery
- `/opds/authors` — author navigation
- `/opds/genres` — genre navigation
- `/opds/ebooks` — EPUB publications
- `/opds/audiobooks` — M4B publications
- `/opds/search?query=...` — title, author, series, and genre search
- `/healthz` — health check

Publication feeds use 50 items per page by default. Use `page` and `per_page` to navigate or adjust the page size, up to 100 items. The scanner walks subdirectories recursively. Deleted files and their extracted covers are removed on the next successful scan.

## Docker

Build and run the minimal static image:

```sh
docker build -t caxton .
docker run --rm -p 8080:8080 \
  -v /path/to/books:/books:ro \
  -v caxton-data:/data \
  caxton
```

The image uses `/books` for the read-only library, `/data/caxton.db` for the persistent SQLite metadata cache, and `/data/covers` for extracted cover files. It supports multi-platform builds through Docker BuildKit's `TARGETOS`, `TARGETARCH`, and `TARGETVARIANT` arguments.

### Docker Compose

Place EPUB and M4B files in `./books`, then start Caxton:

```sh
docker compose up -d --build
```

The catalog is available at `http://localhost:8080/opds`. Metadata is retained in `./data/caxton.db`, while the books directory is mounted read-only. To use another host port or scan interval:

```sh
CAXTON_PORT=8090 CAXTON_SCAN_INTERVAL=1m docker compose up -d
```

Stop the server with `docker compose down`. This preserves the SQLite database in `./data`.
