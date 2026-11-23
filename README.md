<p align="center">
  <img src="docs/logo.png" alt="Caxton" width="400">
</p>

Caxton is a small, read-only OPDS 1.2 and 2.0 server for EPUB ebooks and M4B audiobooks. It scans a directory every five minutes by default, stores extracted metadata in SQLite, and caches embedded cover images as ordinary files. Books are parsed only when their path, size, or modification time changes.

## Run

```sh
go run ./cmd/caxton -library /path/to/books
```

Open `http://localhost:8080/opds/v2` in an OPDS 2.0 client, or `http://localhost:8080/opds/v1` in an OPDS 1.2 client. Both catalogs are always available; no configuration change is needed. `/opds` redirects to the OPDS 2.0 catalog.

Options can be flags or environment variables:

| Flag | Environment | Default |
| --- | --- | --- |
| `-library` | `CAXTON_LIBRARY` | `./library` |
| `-database` | `CAXTON_DATABASE` | `./caxton.db` |
| `-cover-cache` | `CAXTON_COVER_CACHE` | `./covers` |
| `-listen` | `CAXTON_LISTEN` | `:8080` |
| `-scan-interval` | `CAXTON_SCAN_INTERVAL` | `5m` |
| `-base-url` | `CAXTON_BASE_URL` | inferred from request |

## HTTP API

Caxton follows the OPDS specifications rather than defining a separate REST API. OPDS 1.2 and 2.0 expose the same catalog through different representations:

| Version | Entry point | Content type |
| --- | --- | --- |
| OPDS 1.2 | `/opds/v1` | `application/atom+xml;profile=opds-catalog` with the appropriate `kind` parameter |
| OPDS 2.0 | `/opds/v2` | `application/opds+json` |

`/opds` redirects to `/opds/v2`. Old `/opds/books`-style paths and the former `/opds1` namespace are not supported.

The paths below use `{version}` to mean either `v1` or `v2`:

| Path | Description |
| --- | --- |
| `/opds/{version}` | Top-level navigation catalog |
| `/opds/{version}/books` | All publications; accepts `author` and `genre` filters |
| `/opds/{version}/recent` | Publications ordered by first discovery |
| `/opds/{version}/authors` | Author navigation |
| `/opds/{version}/genres` | Genre navigation |
| `/opds/{version}/ebooks` | EPUB publications |
| `/opds/{version}/audiobooks` | M4B publications |
| `/opds/{version}/search` | Search by `query`, `title`, or `author` |

The general `query` search matches title, author, series, and genre. The `title` and `author` parameters restrict matching to those fields. The `author` and `genre` filters on `/books` are case-insensitive exact matches.

Publication feeds accept `page` and `per_page`. Pages contain 50 publications by default, and `per_page` is capped at 100. Feed responses include the appropriate self, start, first, last, previous, and next links.

OPDS 1.2 clients can discover search through `/opds/v1/search.xml`, served as `application/opensearchdescription+xml`. OPDS 2.0 advertises its templated search URL directly in the catalog.

Individual OPDS 2.0 publication resources are available at `/opds/v2/publications/{id}` and use `application/opds-publication+json`.

Publication entries link to shared resources outside the versioned feed namespace:

| Path | Description |
| --- | --- |
| `/books/{id}` | Download the EPUB or M4B file using its native media type |
| `/covers/{id}` | Retrieve the embedded cover using its native image type |
| `/healthz` | Health check; returns `text/plain` |

The scanner walks subdirectories recursively. Deleted files and their extracted covers are removed on the next successful scan.

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

The catalog is available at `http://localhost:8080/opds/v2`. Metadata is retained in `./data/caxton.db`, while the books directory is mounted read-only. To use another host port or scan interval:

```sh
CAXTON_PORT=8090 CAXTON_SCAN_INTERVAL=1m docker compose up -d
```

Stop the server with `docker compose down`. This preserves the SQLite database in `./data`.
