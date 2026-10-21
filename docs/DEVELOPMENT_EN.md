# 🛠️ CourseHub Developer Guide (Secondary Development)

> 🌐 本篇为简体中文版 · English edition: [docs/DEVELOPMENT.md](DEVELOPMENT.md) · This is the English edition.

This guide is for developers who want to adapt **CourseHub (docker-liook)** into their own system: it explains the tech stack, the code map, and the core mechanisms, then walks you hands-on through the three most common modifications — **adding a REST endpoint, adding a storage backend, and extending WebSocket messages**.

> For deployment see the [Deployment Guide (Chinese)](DEPLOYMENT.md) / [Deployment Guide (English)](DEPLOYMENT_EN.md); the full API table is in the [README](../README.md#-api-一览供二次开发).

## Table of Contents

- [1. Tech Stack and Prerequisites](#1-tech-stack-and-prerequisites)
- [2. Local Development](#2-local-development)
- [3. Code Map](#3-code-map)
- [4. Core Mechanisms](#4-core-mechanisms)
- [5. Walkthrough 1: Adding a REST Endpoint](#5-walkthrough-1-adding-a-rest-endpoint)
- [6. Walkthrough 2: Adding a Storage Backend](#6-walkthrough-2-adding-a-storage-backend)
- [7. Walkthrough 3: Extending WebSocket Messages](#7-walkthrough-3-new-websocket-message-types)
- [8. Frontend Development](#8-frontend-development)
- [9. Testing](#9-testing)
- [10. Build and Release](#10-build-and-release)
- [11. Safety Rules (Must Follow When Changing Code)](#11-safety-rules-must-follow-when-changing-code)
- [12. Common Customization Scenarios](#12-common-customization-scenarios)

---

## 1. Tech Stack and Prerequisites

| Layer | Technology | Notes |
|---|---|---|
| Backend | Go 1.27+ (`go.mod` requires 1.27.2) | Standard library `net/http` (Go 1.22+ method-pattern routing) + `gorilla/websocket` |
| Database | SQLite (`modernc.org/sqlite`, pure Go, no CGO) | Single writer, `SetMaxOpenConns(1)`, default journal mode (not WAL) |
| Storage | Local directory / S3 (minio-go v7) / WebDAV | Unified `Storage` interface; implement your own and plug it in |
| Transcoding | ffmpeg (optional) | Server-side HLS + AES-128; falls back to MP4 + Range without it |
| Frontend | Vue 3 + Vite 5 + vue-router 4 + hls.js | No UI framework dependency, hand-drawn SVG icons |

Install:

```bash
# Go 1.27+
# Node 20 + pnpm 10 (frontend)
npm i -g pnpm@10
# ffmpeg (only needed if you want to debug transcoding locally; the app runs without it — video degrades to MP4)
```

## 2. Local Development

### 2.1 Run Frontend and Backend Separately (Recommended, changes take effect immediately)

```bash
# Terminal 1: backend (listens on :8080 by default, data dir ./data)
go run .

# Terminal 2: frontend dev server (port 5173, proxies /api /hls /files /ws to 8080)
cd web
pnpm install
pnpm dev
```

Open `http://localhost:5173` in your browser (proxy config in `web/vite.config.js`; API/HLS/files/WebSocket are all forwarded to 8080).

> The backend runs standalone because of build tags: `webdist.go` (embeds `web/dist` by default) and `webdist_noembed.go` (without embedding when built with `-tags noembed`; startup then only serves a "frontend not built" message).

### 2.2 Production Build (Frontend Embedded in the Binary)

```bash
cd web && pnpm build     # generates web/dist (must run first, otherwise go:embed fails outright)
cd .. && go build .      # single static binary with the frontend embedded
```

### 2.3 Quick Verification

```bash
go test -tags noembed ./...   # backend tests without building the frontend
curl http://localhost:8080/healthz   # {"status":"ok"}
```

## 3. Code Map

```
main.go        Entry: wires Config/Storage/Store/Hub, CSP security headers, /healthz, graceful shutdown
config.go      Environment variables → Config struct (JWT_SECRET auto-generated to <DataDir>/.jwt_secret when empty)
session.go     Session cookie (HMAC token, 7 days) + CSRF (sameOrigin) + requireUser/requireRole
user.go        Login/register (3 registration modes)/user management + login rate limiting + storage test endpoint
store.go       SQLite: schema, models (User/Course/Lesson/Message...), all queries
storage.go     Storage interface + local/S3/WebDAV implementations + runtime switching (stg())
handlers.go    All REST route registration + course/lesson/market/media-proxy handlers + authorization helpers
media.go       Video pipeline: direct/chunked upload, ffmpeg → HLS (AES-128), signed URLs, HLS proxy
chat.go        WebSocket Hub: private chat + course group chat + call signaling + rate limits + call busy state
sanitize.go    Rich-text HTML whitelist sanitization (against stored XSS)
security.go    clientIP (reverse-proxy aware), token bucket, body limits, bcrypt timing alignment, secret masking
config.go      Env loading (getenv/getenvInt, JWT_SECRET persistence)
webdist.go     go:embed all:web/dist (not embedded with -tags noembed)
ws_test.go     End-to-end tests (real HTTP + WebSocket)
web/           Vue 3 frontend (Vite)
  src/api.js        fetch wrapper: same-origin Cookie, 401 auto-redirect to /login
  src/ws.js         Global singleton WebSocket + 3s auto-reconnect + onWS subscription
  src/store.js     Frontend state, src/router.js routing, src/style.css design system
  src/views/       Login/Market/MyCourses/CourseDetail/Learn/Messages/Studio/StudioCourse/Admin
  src/components/  VideoPlayer/RichEditor/GroupChat/Modal/ToastHost/Icon/Sticker…
deploy/        systemd unit + env template
scripts/       build-linux.sh / .ps1 cross-compilation
```

## 4. Core Mechanisms

### 4.1 Routing and Authentication

Routes use the Go 1.22+ `ServeMux` method patterns (registered in `routes()` in `handlers.go`):

```go
mux.HandleFunc("POST /api/courses", a.requireRole(a.handleCreateCourse, "teacher", "admin"))
```

- `requireUser(h)`: validates Origin (non-GET, anti-CSRF) → reads the Cookie → verifies the HMAC token → loads the user (must be `active`) → injects `*User` into the handler.
- `requireRole(h, roles...)`: adds a role whitelist on top of `requireUser` (`admin`/`teacher`/`student`).
- Course-level authorization: `canAccessCourse(u, courseID)` (admin: all / teacher: own courses / student: enrolled or published) and `canManageCourse(u, courseID)` (admin or the course's teacher). **Every course-resource operation must go through these two functions first**.
- Session token: HMAC-SHA256 of `u:<uid>:<exp>`, in an HttpOnly + SameSite=Lax Cookie (`coursehub_session`), valid for 7 days; the signing key is `JWT_SECRET` (auto-generated and persisted to `data/.jwt_secret` when left empty).
- CSRF: all non-GET requests require the `Origin` header to match `Host` (non-browser clients like curl without Origin are allowed through).
- `clientIP()`: trusts `X-Forwarded-For`/`X-Real-IP` only when the direct peer is a private/loopback address (forged headers from direct public connections have no effect).

### 4.2 Response and Error Conventions

```go
writeJSON(w, http.StatusOK, v)        // any JSON
writeErr(w, 400, "参数错误")           // → {"error":"参数错误"}
```

List endpoints return `[]` (not `null`) when empty; unmatched `/api/*` returns a JSON 404 instead of the frontend page (see `main.go`).

### 4.3 Storage Abstraction

```go
type Storage interface {
    Put(ctx, key, data) error
    PutStream(ctx, key, r io.Reader, size int64) error  // chunked writes for large files
    Get(ctx, key) (io.ReadCloser, error)
    GetRange(ctx, key, off int64, length int64) (io.ReadCloser, error) // length<=0 reads to the end
    Stat(ctx, key) (int64, error)
    Delete(ctx, key) error
    DeletePrefix(ctx, prefix) error
}
```

- Three implementations: `newLocal` (local directory) / `newS3` (minio-go; endpoint **without a scheme**, HTTPS controlled by `S3_USE_SSL`) / `newWebDAV`.
- Always fetch storage via `a.stg()`: after the admin saves "Site Settings → File Storage", the connection is rebuilt automatically (`storageSig` compares a config signature; on failure it keeps the previous backend).
- The admin "Test connection" button calls `testStorage(cfg)`: writes a probe file, reads it back, deletes it.

### 4.4 Video Pipeline

```
Direct upload POST /api/lessons/:id/video        (body = file bytes, limit MAX_UPLOAD_MB)
Chunked POST /api/lessons/:id/upload/init  → 16MB PUT chunks → /done (resumable; uploadID whitelist ^[a-f0-9]{16,32}$)
   ↓ source file → storage backend src/{lessonID}/source.*
transcodeLesson (media.go):
   download source to temp dir → generate AES-128 key(16B)+IV(16B)+keyinfo → ffmpeg:
   -c:v libx264 -preset veryfast -crf 23 -pix_fmt yuv420p -c:a aac -b:a 128k -ac 2
   -hls_time 10 -hls_playlist_type vod -hls_key_info_file ... -hls_segment_filename seg%05d.ts
   → upload index.m3u8 + seg*.ts + key.bin to hls/{lessonID}/
   ↓ decorateLesson issues signed playback URLs to the frontend
   video_mode=hls → signed /hls/{id}/index.m3u8; without ffmpeg → video_mode=mp4 → /api/lessons/{id}/stream
```

- Signing: `signPath = HMAC-SHA256(secret, "media|<path>|<exp>")` as hex, `?e=<exp>&s=<sig>`, TTL `SIGN_TTL_MINUTES` (default 120 minutes); `checkSign` uses `subtle.ConstantTimeCompare` against timing attacks; `handleHLS` **re-signs** every segment/key URI when serving the m3u8.
- Concurrent transcodes: `TRANSCODE_CONCURRENCY` (default 2, semaphore); **on restart `ResetStuckLessons()` marks "processing" lessons as failed** (transcoding is an in-process task, not persisted).
- Limits: global 1MB JSON body; video `MAX_UPLOAD_MB` (default 4096); docs 200MB; images 10MB; single WS message 1MB.

### 4.5 WebSocket Hub (chat.go)

- `Hub.clients` is `map[userID]map[*client]struct{}`: **one account may hold multiple connections** (multiple tabs); `busy` tracks active calls.
- Client → server: `chat.send` / `chat.read` / `group.send` / `group.read` / `call.offer` / `call.answer` / `call.ice` / `call.end`.
- Server → client: `presence` / `chat.new` / `chat.sent` / `group.new` / `group.sent` / `call.*` / `error`.
- Every message passes rate limiting before `handleMsg` dispatch: chat 2/s (burst 15), signaling 2/s (burst 20), ICE 15/s (burst 120); single message ≤1MB; sending in private/group chat validates the `CanChat` / `CanGroupChat` relationship first.
- The frontend `web/src/ws.js` is a global singleton (one shared connection), auto-reconnects after 3s, and components subscribe via `onWS(fn)`.

### 4.6 Content Security

- Rich text (rich lessons): `sanitize.go` whitelist cleaning — `script/iframe/object` and friends are removed with their subtrees, non-whitelisted tags are unwrapped into their children, event attributes (`on*`) plus `srcset/formaction` are stripped, `a.href`/`img.src` go through a protocol whitelist, and `style` is checked against a regex whitelist.
- All SQL is parameterized (store.go); storage keys/filenames go through the `nameRe` whitelist (`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`, prevents path traversal).
- Rich text must pass `sanitizeHTML()` before being saved — do not bypass it.

## 5. Walkthrough 1: Adding a REST Endpoint

Using "site-wide stats for admins" as an example — 4 steps.

**① Define the DB query (store.go)** — just compose existing queries:

```go
// store.go
func (s *Store) SiteStats() (users, courses, lessons int64, err error) {
	err = s.db.QueryRow(
		`SELECT (SELECT COUNT(*) FROM users), (SELECT COUNT(*) FROM courses), (SELECT COUNT(*) FROM lessons)`,
	).Scan(&users, &courses, &lessons)
	return
}
```

> All table definitions live in the `schema` string at the top of `store.go`; when **changing the schema**, add columns with `ALTER TABLE ... ADD COLUMN` in the migration area (the project uses `CREATE TABLE IF NOT EXISTS` plus startup column backfills for automatic migration — see the existing migration code in store.go).

**② Write the handler (handlers.go)** — follow the standard pattern: parse input → authorize → query → `writeJSON`/`writeErr`:

```go
func (a *API) handleSiteStats(w http.ResponseWriter, r *http.Request, u *User) {
	users, courses, lessons, err := a.db.SiteStats()
	if err != nil {
		writeErr(w, 500, "查询失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{
		"users": users, "courses": courses, "lessons": lessons,
	})
}
```

**③ Register the route (in `routes()`, handlers.go)**:

```go
mux.HandleFunc("GET /api/stats/site", a.requireRole(a.handleSiteStats, "admin"))
```

**④ Write a test (ws_test.go)**:

```go
func TestSiteStats(t *testing.T) {
	api, h := newTestAPI(t)
	ts := httptest.NewServer(h)
	testBase = ts.URL
	defer ts.Close()

	admin := newClient()
	if resp := postJSON(t, admin, "/api/login", map[string]string{"username": "admin", "password": "admin-pass-123"}); resp.StatusCode != 200 {
		t.Fatal("admin login failed")
	}
	resp := getJSON(t, admin, "/api/stats/site")   // getJSON is an existing helper in ws_test.go
	if resp.StatusCode != 200 {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
}
```

**⑤ Call it from the frontend** (`api()` in `web/src/api.js` already handles 401 → redirect to login):

```js
import { api } from './api'
const stats = await api('/api/stats/site')
```

## 6. Walkthrough 2: Adding a Storage Backend

The storage layer is a single interface (`storage.go`); implement it to plug in any backend (e.g. Azure Blob, native OSS SDK):

```go
type Storage interface {
	Put(ctx context.Context, key string, data []byte) error
	PutStream(ctx context.Context, key string, r io.Reader, size int64) error // large files
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	GetRange(ctx context.Context, key string, off, length int64) (io.ReadCloser, error) // length<=0 reads to the end; HLS/doc Range playback depends on it
	Stat(ctx context.Context, key string) (int64, error)
	Delete(ctx context.Context, key string) error
	DeletePrefix(ctx context.Context, prefix string) error // cleanup when deleting lessons/courses
}
```

Integration steps:

1. Create `storage_azure.go` implementing the 7 methods above (mirror `s3Store`: map `GetRange` straight onto HTTP Range; chunk `PutStream`).
2. Register it in the `NewStorage` switch:

```go
case "azure":
    return newAzure(cfg)
```

3. Add `Config` fields (`config.go` reads env vars, e.g. `AZURE_CONTAINER`) and mirror the comments in compose/`.env.example`.
4. The admin "Site Settings → File Storage" form lives in `web/src/views/Admin.vue`; the backend receiver is `handleStorageTest`/`storageReq.applyTo` (user.go) — to support switching from the admin UI, add the fields to the `Settings` model (store.go) and to `storageSig` as well.
5. Verify with the admin "Test connection" button (goes through `testStorage`, which writes a probe file and reads it back).

Note: the S3 `minio.New()` endpoint must **not** include `http://` (HTTPS is decided by `S3_USE_SSL`); all keys go through the `nameRe` whitelist, so new backends don't need their own path validation.

## 7. Walkthrough 3: New WebSocket Message Types

All WS logic lives in `chat.go`: connections enter `Hub.clients` (one account can hold multiple connections across tabs), and dispatch happens in `Hub.handleMsg`.

To add a message type (e.g. an in-course broadcast `lesson.notify`):

```go
// 1. Add a field to wsMsg (top of chat.go)
type wsMsg struct {
    // ...existing fields
    LessonID int64 `json:"lesson_id,omitempty"`
}

// 2. Add a dispatch case in handleMsg
case "lesson.notify":
    if !h.api.db.CanManageLesson(c.user, m.Group) { /* permission check, mirror CanGroupChat */ }
    h.sendTo(m.To, serverMsg{Type: "lesson.notify", Message: m.Msg.Content})

// 3. Subscribe from the frontend (any component)
import { onWS } from '../ws.js'
onWS((m) => { if (m.type === 'lesson.notify') showToast(m.message) })
```

Rules: every message entering `handleMsg` is rate-limited first (grouped by `m.Type`); unknown types get an `error` reply; **message content is capped at 8000 characters and 1MB per message**; new types can extend the `serverMsg` struct with extra fields; the frontend `ws.js` is a global singleton with `onWS` subscription and 3s auto-reconnect.

## 8. Frontend Development

- **Stack**: Vue 3 + Vite 5 + vue-router 4 (`createWebHistory`), no UI component library; styles in `src/style.css` (Xiaoe-Tech-style design system).
- **API wrapper**: `src/api.js` — `api(path, {json|body})` attaches the Cookie automatically, throws `data.error` on non-2xx, redirects to `/login` on 401; `rawPut`/`postForm` are convenience wrappers.
- **WebSocket**: `src/ws.js` is a global singleton — `onWS(fn)` to subscribe, 3s reconnect on drop; chat/call components hang off `Messages.vue` / `GroupChat.vue`.
- **Adding a page**: add a route in `src/router.js` → create `src/views/Xxx.vue` (model it on `Market.vue`) → add the nav entry in `App.vue`.
- **Hand-drawn icons**: all site icons are inline SVGs in `components/Icon.vue` — add a `<path>` for a new icon; stickers live in `src/stickers.js`.
- After a production build `web/dist` gets embedded; for local debugging use `pnpm dev` (proxies for `/api`, `/hls`, `/files`, `/ws` are preconfigured).

## 9. Testing

Tests live in `ws_test.go` using a "real HTTP + WebSocket" pattern: `newTestAPI` spins up temp storage + SQLite (`t.TempDir()`) + an `httptest.Server`, then drives the full flow (login → create course → upload → chat → signaling) with a cookiejar-equipped `http.Client` acting as a browser.

```bash
go test ./...                    # full suite (requires cd web && pnpm build first, because of go:embed)
go test -tags noembed ./...      # works without building the frontend
go test -run TestRBAC -v ./...   # run a single test
```

Please add end-to-end tests for new features following the existing `newTestAPI` + `postJSON/getJSON` pattern; CI (`.github/workflows/ci.yml`) runs the gofmt check → go vet → go test → full build.

## 10. Build and Release

```bash
# Local production build (Linux binaries, cross-compilable)
./scripts/build-linux.sh amd64          # → dist/course-hub-linux-amd64 (use build-linux.ps1 on Windows)

# Docker (multi-stage: frontend → static Go build → alpine runtime image with ffmpeg baked in)
docker build -t course-hub .

# Release a new version: tag it and CI builds/pushes images to GHCR automatically (semver tags + latest)
git tag v1.1.0 && git push origin v1.1.0
```

- CI (`.github/workflows/ci.yml`): **builds the frontend first** (go:embed depends on it) → gofmt check → go vet → go test → go build. Run `gofmt -w .` locally before committing to keep CI green.
- `docker.yml` builds amd64+arm64 images and pushes them to GHCR on pushes to main / `v*` tags (`latest` + semver tags).

## 11. Safety Rules (Must Follow When Changing Code)

1. **Every new endpoint must be wrapped in `requireUser` / `requireRole`**, and course resources must pass `canAccessCourse` / `canManageCourse` first;
2. Non-GET requests automatically get the Origin check (inside `requireUser`) — **do not bypass it by registering handlers bare with `mux.HandleFunc`**;
3. All SQL must be parameterized (`?` placeholders); never concatenate strings; schema changes belong in store.go's schema/migrations;
4. User-supplied filenames/storage keys must pass the `nameRe` whitelist; always build paths with `path.Join`;
5. Rich text must go through `sanitizeHTML()` before storage; image/document uploads must be size-limited with `MaxBytesReader`;
6. Media URLs must always be issued via `signedURL()` — never expose direct storage links; never make storage buckets publicly readable;
7. New WS message types must add a rate-limit branch in `handleMsg`, keep payloads ≤8000 characters, and enforce relationship checks (`CanChat`/`CanGroupChat`);
8. Never return `PasswordHash` / storage secrets in responses (models already use `json:"-"`, and `maskSecret` masks values);
9. If you change rate limiting/auth logic, update `SECURITY.md` and the corresponding tests in the same change;
10. Before committing: `gofmt -w .` + `go vet ./...` + `go test -tags noembed ./...` (CI checks all of it).

## 12. Common Customization Scenarios

| What you want to do | Main touchpoints |
|---|---|
| Add a course field (e.g. tags) | `store.go` (schema + model + migration) → `handlers.go` handler → `web/src` display |
| Switch to PostgreSQL | Swap the driver in `store.go`; note the **single-writer assumption** (`MaxOpenConns(1)`; in-memory rate limiting/transcode queue assume a single instance — see the engineering roadmap in the README) |
| Multi-bitrate transcoding | ffmpeg args in `media.go` (currently single-bitrate libx264 veryfast crf23); add multiple outputs + a master m3u8 |
| SSO / enterprise login | `user.go` register/login handlers + user table fields in `store.go`; sessions reuse `setSessionCookie` |
| Add payments | Needs an order model + callback endpoints + course price fields; mind CORS/CSRF and signature verification |
| Plug in your own object storage | Just implement the Section 6 interface — frontend/transcoding/signing all keep working unchanged |
| Add message push | The Hub's `sendTo` in `chat.go` is the single outlet; hooking a webhook there captures all message events |
| New lesson type | Extend `Lesson.Kind` → add URL logic in `decorateLesson` (media.go) → branch rendering in `Learn.vue` / `StudioCourse.vue` |

> For the fuller "not done yet" list (multi-bitrate, learning progress, watermarking, PostgreSQL, audit logs…), see [README → roadmap](../README.md#-尚未完善--后续计划) — PRs welcome.

> 🌐 This is the English edition · 本篇为简体中文版：[docs/DEVELOPMENT.md](DEVELOPMENT.md)
