# @myelophone/goserver

`@myelophone/goserver` is a fast, minimalist, production-oriented web framework for Go. It provides expressive routing, composable middleware, request parsing and validation, secure responses, sessions, caching, database integrations, background jobs, realtime communication, and built-in operational tooling while preserving the familiar `net/http` programming model.

> [!TIP]
> **Need a ready-to-run backend instead of assembling a project from scratch?**
> [`@myelophone/goserver-template`](https://github.com/myelophone/goserver-template) is the official backend boilerplate built on `@myelophone/goserver`. It provides a prepared application structure and starting point for quickly developing a new service.
>
> **[Open template repository](https://github.com/myelophone/goserver-template)** · **[Create a repository from this template](https://github.com/myelophone/goserver-template/generate)**
>
> ```bash
> git clone https://github.com/myelophone/goserver-template.git my-backend
> cd my-backend
> ```

Designed for high-load backend services, REST APIs, web applications, and internal platforms, `@myelophone/goserver` combines a compact API with a complete server lifecycle. Handlers remain ordinary `http.HandlerFunc` values, middleware uses the standard Go signature, and each larger subsystem is optional and replaceable through a narrow interface.

The defaults are intentionally opinionated: request IDs, access logging, load shedding, panic recovery, rate limiting, static assets, redirects, request filtering, URL sanitization, CSRF protection, security headers, timeouts, body limits, and client classification are installed together. Idempotency must be installed explicitly after route authentication and authorization. If those defaults do not fit a service, construct a smaller stack one middleware at a time.

The implementation is performance-conscious and includes pooled buffers, bounded LRU caches, singleflight request coalescing, connection limits, graceful shutdown, and overload protection. As with any Go HTTP stack, actual allocations and throughput depend on the handlers, middleware, encoders, and deployment; benchmark your own workload before making latency or allocation guarantees.

## Contents

- [Requirements and installation](#requirements-and-installation)
- [Why @myelophone/goserver](#why-myelophonegoserver)
- [Quick start](#quick-start)
- [How the server is assembled](#how-the-server-is-assembled)
- [Extending @myelophone/goserver](#extending-myelophonegoserver)
- [Routing and route groups](#routing-and-route-groups)
- [Requests, validation, and responses](#requests-validation-and-responses)
- [Streaming responses](#streaming-responses)
- [Middleware](#middleware)
- [Errors, hooks, and background work](#errors-hooks-and-background-work)
- [Cookies, sessions, JWT, and encryption](#cookies-sessions-jwt-and-encryption)
- [Caching and idempotency](#caching-and-idempotency)
- [PostgreSQL](#postgresql)
- [Redis](#redis)
- [Templates, static assets, and i18n](#templates-static-assets-and-i18n)
- [Nuxt-like web application](#nuxt-like-web-application)
- [Outbound HTTP, proxies, and HTML parsing](#outbound-http-proxies-and-html-parsing)
- [WebSockets](#websockets)
- [Cron, email, Telegram, and multi-tenancy](#cron-email-telegram-and-multi-tenancy)
- [Built-in third-party integrations](#built-in-third-party-integrations)
- [Health, metrics, profiling, and operations](#health-metrics-profiling-and-operations)
- [Configuration](#configuration)
- [Utility API](#utility-api)
- [Development](#development)
- [GitHub Actions](#github-actions)
- [Complete copy-paste example](#complete-copy-paste-example)
- [License](#license)

## Requirements and installation

- Go `1.27` or newer, matching [`go.mod`](./go.mod).
- Node.js is required only for web setup, development and compilation; the production distribution does not need it.
- PostgreSQL and Redis are optional and only needed for their corresponding packages.
- Docker is optional.

Install the module in an application:

```bash
go get github.com/myelophone/goserver
```

The repository itself includes a runnable example in [`cmd/main.go`](./cmd/main.go):

```bash
cp .env.example .env
go run -tags webcli ./cmd generate
go run ./cmd
```

The server listens on `:8080` unless `HTTP_PORT` is changed.

`NewServer` accepts a port with or without a leading colon. `NewServer("8080")`, `NewServer(":8080")`, and an accidentally duplicated `NewServer("::8080")` all listen on `:8080`. Complete addresses such as `127.0.0.1:8080` and `[::1]:8080` are preserved unchanged.

## Why @myelophone/goserver

`@myelophone/goserver` makes HTTP services quick to build without introducing a custom request context or a large abstraction layer. Its API stays close to the standard library while covering the complete lifecycle of a production backend—from accepting and validating a request to overload protection, monitoring, background work, and graceful shutdown.

|                      | `net/http` alone         | Typical minimalist router        | `@myelophone/goserver`                                                       |
| -------------------- | ------------------------ | -------------------------------- | ---------------------------------------------------------------------------- |
| Handlers             | Standard Go handlers     | Often framework-specific context | Standard Go handlers                                                         |
| Routing              | Manual mux setup         | Concise methods and parameters   | Concise methods, parameters, wildcards, and groups                           |
| Middleware           | Compose yourself         | Usually supported                | Standard middleware plus a production stack                                  |
| Overload controls    | Build yourself           | Usually external                 | Connection limits, load shedding, rate limits, deadlines, body limits        |
| Application services | Choose and wire packages | Usually external                 | Optional cache, sessions, DB/Redis, cron, mail, i18n, templates, HTTP client |
| Operations           | Build yourself           | Usually external                 | Health, Prometheus metrics, pprof, request IDs, graceful shutdown/reload     |

`@myelophone/goserver` is not intended to replace the Go ecosystem. It composes with it: any normal `http.Handler`, pgx query, Redis-backed adapter, logger, reverse proxy, Prometheus scraper, or custom middleware can remain ordinary Go code.

## Quick start

```go
package main

import (
	"net/http"

	"github.com/myelophone/goserver"
)

func main() {
	s := goserver.NewServer(goserver.GetEnv("HTTP_PORT", "8080"))

	// Installs the opinionated production middleware set.
	s.Defaults()

	s.GET("/", func(w http.ResponseWriter, r *http.Request) {
		s.RespondJSON(w, r, map[string]any{
			"name":    "@myelophone/goserver",
			"status":  "ok",
			"request": goserver.GetRequestID(r.Context()),
		})
	})

	s.GET("/users/:id", func(w http.ResponseWriter, r *http.Request) {
		id := s.GetParams(r).Get("id")
		s.RespondJSON(w, r, map[string]string{"id": id})
	})

	// Adds /healthz and /metricz, server context, optional gzip/rate
	// limiting, then blocks until graceful shutdown.
	s.Run()
}
```

Try it:

```bash
curl -A 'Mozilla/5.0' http://localhost:8080/
curl -A 'Mozilla/5.0' http://localhost:8080/users/42
curl -A 'Mozilla/5.0' http://localhost:8080/healthz
```

`Defaults()` rejects empty user agents and several automation/scanner user agents, including curl's default one. Use a browser-like `User-Agent` while testing, omit `MaliciousRequestMiddleware` in a custom middleware stack, or change that policy in your application fork.

## How the server is assembled

There are two supported assembly styles.

### Opinionated defaults

Call `Defaults()` before registering routes, then call `Run()`:

```go
s := goserver.NewServer("8080")
s.Defaults()
s.GET("/", home)
s.Run()
```

`Defaults()` installs, in order:

1. request ID;
2. development or production request logging;
3. slow-request metrics logging;
4. load shedding;
5. panic recovery;
6. rate limiting;
7. favicon and static asset handling;
8. API-prefix handling;
9. canonical redirects;
10. outdated-client and malicious-request filters;
11. blocked-path and URL sanitization filters;
12. CSRF protection and security headers;
13. handler timeout and body-size limit;
14. bot/AI detection;
15. Idempotency is opt-in on authenticated routes (see below).

It also registers `/robots.txt` and, when metrics are enabled, protected pprof routes. `Run()` adds `/healthz`, `/metricz`, server context, optional gzip, and another configured rate-limiter layer before starting the listener.

### Custom stack

Use only the middleware your service needs. Middleware is executed in registration order: the first `Use` call is the outermost wrapper.

```go
s := goserver.NewServer("8080")
s.Use(s.RequestIDMiddleware)
s.Use(s.ProdAccessLogger)
s.Use(s.RecoveryMiddleware(nil))
s.Use(s.ServerContextMiddleware)
s.Use(s.SecurityMiddleware(goserver.SecurityOptions{
	FrameOptions:       "DENY",
	ContentTypeNoSniff: true,
	ReferrerPolicy:     "no-referrer",
	CSP:                "default-src 'self'",
}))
s.Use(s.LimitBodyMiddleware(2 << 20))
s.GET("/", home)
s.Run()
```

`Run()` always adds its built-in operational routes and final middleware. Use `Start()` instead only when you deliberately want to skip the routes and final middleware added by `Run()`.

## Extending @myelophone/goserver

`@myelophone/goserver` is extended through standard Go handlers and middleware, route groups, replaceable storage/provider interfaces, and lifecycle hooks. Application-specific functionality stays in regular Go packages and can be installed on a server explicitly during startup.

### Write custom middleware

A middleware receives the next `http.Handler` and returns a handler. Install it globally with `Server.Use` or only on a `RouteGroup`.

```go
func RequireAPIKey(expected string) goserver.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if subtle.ConstantTimeCompare(
				[]byte(r.Header.Get("X-API-Key")),
				[]byte(expected),
			) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			w.Header().Set("X-Service", "billing")
			next.ServeHTTP(w, r)
		})
	}
}

admin := s.Group("/admin")
admin.Use(RequireAPIKey(os.Getenv("API_KEY")))
admin.GET("/stats", func(w http.ResponseWriter, r *http.Request) {
	s.RespondJSON(w, r, s.GetStats())
})
```

Middleware registration order matters: the first `Use` call becomes the outermost wrapper. A middleware should normally call `next.ServeHTTP` exactly once, unless it deliberately terminates the request with a response.

### Mount existing net/http code

An existing standard-library or third-party handler can be registered without an adapter layer:

```go
files := http.StripPrefix("/downloads/", http.FileServer(http.Dir("./downloads")))
s.GET("/downloads/*path", files.ServeHTTP)

standardHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("served by a standard net/http handler"))
})
s.GET("/standard", standardHandler.ServeHTTP)
```

### Replace subsystems through interfaces

Implement one of the public interfaces to connect your own infrastructure:

| Interface        | Use it to add                                                                   |
| ---------------- | ------------------------------------------------------------------------------- |
| `CacheStore`     | Memcached, DynamoDB, another Redis client, or a service-specific cache.         |
| `SessionStorage` | SQL, encrypted cookies, a remote session service, or another distributed store. |
| `ProxyProvider`  | A proxy vendor or an internal proxy inventory API.                              |
| `WSBroker`       | Redis Pub/Sub, NATS, Kafka, or another WebSocket fan-out transport.             |
| `DBQuerier`      | A pgx pool, transaction, or compatible test double for database helpers.        |

The bundled in-memory and Redis implementations use these same interfaces, so an application can switch infrastructure without changing handler code. Assign a `CacheStore` to `s.Cache`, pass a `SessionStorage` to `s.SetSessionStorage`, provide one or more `ProxyProvider` values to `NewProxyManager`, or attach a `WSBroker` with `WebSocketHub.SetBroker`.

### Add lifecycle behavior

- `RegisterHook` / `RegisterHookWithPriority` add reusable server setup modules; call `ApplyHooks` after registering them.
- `RegisterBeforeRequest`, `RegisterAfterRequest`, `RegisterOnError`, and `RegisterOnShutdown` attach lifecycle callbacks.
- `SetLogger` accepts a custom `*log.Logger`.
- `ErrorNotifier` connects error reporting or alerting.
- `RunAsync` starts panic-protected work that graceful shutdown waits for.
- `SetNotFoundHandler` and `SetErrorHandler` replace default application errors.

Hooks can install routes and shutdown behavior as a self-contained module:

```go
goserver.RegisterHookWithPriority(20, func(s *goserver.Server) {
	s.GET("/ready", func(w http.ResponseWriter, r *http.Request) {
		s.RespondJSON(w, r, map[string]string{"status": "ready"})
	})
})
goserver.RegisterOnShutdown(func() {
	log.Println("application resources closed")
})

s.ApplyHooks()
```

## Routing and route groups

The router supports static paths, named segments, and a trailing wildcard. Paths are cleaned, and non-root trailing slashes are normalized.

```go
s.GET("/articles/:slug", func(w http.ResponseWriter, r *http.Request) {
	params := s.GetParams(r)
	s.RespondJSON(w, r, map[string]string{"slug": params.Get("slug")})
})

s.GET("/files/*path", func(w http.ResponseWriter, r *http.Request) {
	s.RespondText(w, s.GetParams(r).Get("path"))
})
```

Available registration methods are `GET`, `POST`, `PUT`, `PATCH`, `DELETE`, `HEAD`, `OPTIONS`, and `ANY`. `ANY` registers all of those methods. An `OPTIONS` request to a known route receives `204 No Content` and an `Allow` header when no explicit OPTIONS handler matches.

Route groups combine a prefix with group-local middleware:

```go
func requireBearer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer demo" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

api := s.Group("/api/v1")
api.Use(requireBearer)
api.GET("/profile", profileHandler)
api.POST("/orders", createOrderHandler)
```

Set a custom fallback or error handler before starting:

```go
s.SetNotFoundHandler(func(w http.ResponseWriter, r *http.Request) {
	s.RenderErrorJSON(w, r, http.StatusNotFound, "route does not exist")
})

s.SetErrorHandler(func(w http.ResponseWriter, r *http.Request) {
	s.RenderErrorJSON(w, r, http.StatusInternalServerError, "unexpected failure")
})
```

Set `API_PREFIX=/api` to expose registered `/users` as `/api/users`; register routes without the prefix. Values `api`, `/api`, and `/api/` are normalized to the same prefix.

## Requests, validation, and responses

### Unified parsing for JSON and every common form format

`ParseRequest` converts API payloads and browser forms into one `ParsedRequest` representation. A handler does not need separate parsing branches for JSON, URL-encoded forms, and multipart forms: ordinary values are available through the same `Fields` map and typed getters, while uploaded files are available through `Files`.

| Incoming content type               | Unified result                                                                                                                 |
| ----------------------------------- | ------------------------------------------------------------------------------------------------------------------------------ |
| `application/json`                  | JSON object properties are decoded into `ParsedRequest.Fields`. Nested objects and arrays remain structured Go values.         |
| `application/x-www-form-urlencoded` | Form controls are copied into `Fields`; a single value becomes a string and repeated controls become `[]string`.               |
| `multipart/form-data`               | Text controls use the same `Fields` representation, while uploaded files are grouped by control name in `ParsedRequest.Files`. |
| Other content types                 | The body is preserved as `ParsedRequest.Raw` for custom protocols or manual decoding.                                          |

This normalization makes the same validation and business logic reusable across an HTML form, a JavaScript `FormData` request, and a JSON API client. `GetString`, `GetInt`, `GetFloat`, `GetBool`, `GetSlice`, `GetMap`, and `GetFiles` provide a consistent access layer after parsing.

Calling `Validate` normalizes values further according to `ValidationRule`: numeric strings become `int` or `float64`, boolean strings become `bool`, defaults are inserted into `Fields`, nested objects and slices are checked recursively, enums and regular expressions are enforced, and uploaded files can be restricted by MIME type and size.

`ParseRequest` requires `ServerContextMiddleware`, which `Run()` installs automatically. `Defaults()` also installs the configured request-body limit before application handlers.

```go
s.POST("/users", func(w http.ResponseWriter, r *http.Request) {
	input, err := goserver.ParseRequest(r)
	if err != nil {
		s.RenderErrorJSON(w, r, http.StatusBadRequest, err.Error())
		return
	}

	rules := []goserver.ValidationRule{
		{Name: "email", Type: "string", Required: true, Regex: goserver.RegexEmail},
		{Name: "age", Type: "int", Required: true, Min: goserver.FloatPtr(18), Max: goserver.FloatPtr(130)},
		{Name: "role", Type: "string", Default: "reader", Enum: []any{"reader", "editor"}},
		{Name: "tags", Type: "slice", ElemType: "string"},
		{Name: "profile", Type: "object", Nested: []goserver.ValidationRule{
			{Name: "display_name", Type: "string", Required: true, MinLen: goserver.IntPtr(2)},
		}},
	}

	if validationErrors := input.Validate(rules); len(validationErrors) != 0 {
		s.RenderErrorJSON(w, r, http.StatusUnprocessableEntity, validationErrors[0].Error())
		return
	}

	age, ok := input.GetInt("age")
	if !ok {
		s.RenderErrorJSON(w, r, http.StatusUnprocessableEntity, "age must be an integer")
		return
	}
	s.RespondJSON(w, r, map[string]any{
		"email": input.GetString("email"),
		"age":   age,
		"role":  input.GetString("role"),
	})
})
```

Browser controls, including `<input type="number" name="age">`, are submitted as text. For example, the browser sends `age=37`; `GetInt` can parse that numeric string directly, and the `Type: "int"` validation rule additionally replaces `Fields["age"]` with the Go `int` value `37`. In this handler validation runs first, so business logic receives the normalized type. The explicit `ok` check keeps the handler safe even if its validation rules are changed later.

The `/users` handler above accepts all of these requests without changing its parsing or validation code:

```bash
# JSON API
curl -A 'Mozilla/5.0' -H 'Content-Type: application/json' \
  -d '{"email":"ada@example.com","age":37,"role":"editor"}' \
  http://localhost:8080/users

# Traditional browser form
curl -A 'Mozilla/5.0' -H 'Content-Type: application/x-www-form-urlencoded' \
  --data-urlencode 'email=ada@example.com' --data-urlencode 'age=37' \
  http://localhost:8080/users

# Multipart form, including a file control
curl -A 'Mozilla/5.0' \
  -F 'email=ada@example.com' -F 'age=37' -F 'avatar=@avatar.png' \
  http://localhost:8080/users
```

To validate an upload, add a file rule and retrieve the accepted files by the same form-control name:

```go
rules := []goserver.ValidationRule{
	{Name: "avatar", Type: "file", Required: true,
		AllowedMime: []string{"image/jpeg", "image/png"},
		MaxFileBytes: 5 << 20},
}

if validationErrors := input.Validate(rules); len(validationErrors) != 0 {
	s.RenderErrorJSON(w, r, http.StatusUnprocessableEntity, validationErrors[0].Error())
	return
}

avatars := input.GetFiles("avatar")
```

For non-form content types, inspect `input.Raw` and apply the decoder required by the application protocol.

### Response helpers

```go
s.RespondJSON(w, r, value)             // application/json
s.RespondRawJSON(w, []byte(`{"ok":true}`))
s.RespondSecureJSON(w, r, value)       // prefixes while(1); against JSON hijacking
s.RespondAsciiJSON(w, r, value)        // escapes non-ASCII bytes
s.RespondXML(w, r, value)
s.RespondHTML(w, "<h1>Hello</h1>")
s.RespondText(w, "hello")
s.RespondAsItIs(w, "already formatted")
```

Headers, caching, redirects, and files:

```go
s.SetHeaders(w, map[string]string{"X-App": "demo"})
s.MergeHeaders(w, map[string]string{"Cache-Control": "no-store"})
s.SetCache(w, "public, max-age=60")
s.AddPreloadHeaders(w, []string{"/assets/app.css", "/assets/app.js"})
s.Redirect(w, r, http.StatusTemporaryRedirect, "/login")
s.ServeFile(w, r, "./reports/latest.pdf")
s.DownloadFile(w, r, "./reports/latest.pdf", "report.pdf")
```

Set a response header for every request with the packaged middleware:

```go
s.Use(goserver.ResponseHeader("X-Powered-By", "@myelophone/goserver"))
```

## Streaming responses

`ResponseStream` sends a response incrementally over an HTTP connection. It sets the headers that disable proxy and browser buffering, flushes headers immediately, and writes each chunk as soon as the handler produces it. The server disables gzip compression automatically for a stream, even when `GzipMiddleware` is active elsewhere.

Create a typed stream:

```go
stream, err := s.NewHTMLStream(w, r)   // text/html; charset=utf-8
stream, err := s.NewJSONStream(w, r)   // application/json; charset=utf-8
stream, err := s.NewSSEStream(w, r)    // text/event-stream; charset=utf-8
stream, err := s.NewNDJSONStream(w, r) // application/x-ndjson; charset=utf-8
```

Use `NewStreamWithStatus` when the stream needs a non-200 status code:

```go
stream, err := s.NewStreamWithStatus(w, r, "text/html; charset=utf-8", http.StatusAccepted)
```

### HTML streaming (SSR)

`WriteHTML` sends a chunk and flushes it immediately. The browser can render partial markup while the handler is still running.

```go
s.GET("/stream-example", func(w http.ResponseWriter, r *http.Request) {
	stream, err := s.NewHTMLStream(w, r)
	if err != nil {
		http.Error(w, "stream not supported", http.StatusInternalServerError)
		return
	}

	stream.WriteHTML("<!doctype html><html><body><h1>Loading...</h1>")

	for i := 1; i <= 5; i++ {
		if stream.IsClosed() {
			return
		}
		time.Sleep(500 * time.Millisecond)
		stream.WriteHTML(fmt.Sprintf("<p>Section %d loaded</p>", i))
	}

	stream.WriteHTML("</body></html>")
})
```

### Streaming JSON arrays

`StartJSONArray`, `WriteJSONArrayItem`, and `EndJSONArray` stream a JSON array one element at a time.

```go
s.GET("/api/stream-data", func(w http.ResponseWriter, r *http.Request) {
	stream, err := s.NewJSONStream(w, r)
	if err != nil {
		http.Error(w, "stream not supported", http.StatusInternalServerError)
		return
	}

	stream.StartJSONArray()

	for i := 1; i <= 10; i++ {
		if stream.IsClosed() {
			return
		}
		time.Sleep(200 * time.Millisecond)
		stream.WriteJSONArrayItem(map[string]any{
			"id":   i,
			"name": fmt.Sprintf("Item %d", i),
		})
	}

	stream.EndJSONArray()
})
```

### Server-Sent Events

`WriteSSEEvent` formats and sends a single SSE message. The `Data` field can be a string, `[]byte`, or any JSON-marshalable value.

```go
s.GET("/api/sse", func(w http.ResponseWriter, r *http.Request) {
	stream, err := s.NewSSEStream(w, r)
	if err != nil {
		http.Error(w, "stream not supported", http.StatusInternalServerError)
		return
	}

	stream.WriteSSEEvent(goserver.SSEEvent{Event: "connected", Data: "ready"})

	for i := 1; i <= 10; i++ {
		if stream.IsClosed() {
			return
		}
		time.Sleep(1 * time.Second)
		stream.WriteSSEEvent(goserver.SSEEvent{
			ID:    fmt.Sprintf("event-%d", i),
			Event: "update",
			Data: map[string]any{
				"count": i,
				"time":  time.Now().Format(time.RFC3339),
			},
		})
	}

	stream.WriteSSEEvent(goserver.SSEEvent{Event: "complete", Data: "done"})
})
```

### NDJSON

`WriteNDJSON` marshals a value and appends a newline. This format is useful for log streaming.

```go
s.GET("/api/logs", func(w http.ResponseWriter, r *http.Request) {
	stream, err := s.NewNDJSONStream(w, r)
	if err != nil {
		http.Error(w, "stream not supported", http.StatusInternalServerError)
		return
	}

	for i := 1; i <= 20; i++ {
		if stream.IsClosed() {
			return
		}
		time.Sleep(100 * time.Millisecond)
		stream.WriteNDJSON(map[string]any{
			"level":   "INFO",
			"message": fmt.Sprintf("Processing item %d", i),
			"time":    time.Now().Format(time.RFC3339),
		})
	}
})
```

### Interaction with middleware

`TimeoutMiddleware` and `MetricsMiddleware` recognize streaming responses and handle them correctly:

- `TimeoutMiddleware` keeps the deadline on the request context but does not interrupt an active stream; the stream checks `IsClosed()` to stop early when the client disconnects or the context is cancelled.
- `MetricsMiddleware` does not log streaming requests as slow requests, because their long duration is expected.

## Middleware

Every middleware uses the standard shape `func(http.Handler) http.Handler`. Built-in middleware can be installed globally with `Use`, locally with `RouteGroup.Use`, or composed manually.

| Middleware                                | Purpose                                                                          |
| ----------------------------------------- | -------------------------------------------------------------------------------- |
| `RequestIDMiddleware`                     | Generates an xid and exposes it through `X-Request-ID` and `GetRequestID`.       |
| `LogRequest` / `ProdAccessLogger`         | Development memory-delta log or production status/access log.                    |
| `MetricsMiddleware`                       | Logs handlers slower than 100 ms.                                                |
| `LoadSheddingMiddleware`                  | Rejects above `CONCURRENCY_LIMIT` with 503 and `Retry-After`.                    |
| `RecoveryMiddleware`                      | Recovers panics, calls error hooks, and renders a 500 response.                  |
| `WithRateLimiter` / `RateLimitMiddleware` | Per-IP fixed-window LRU limiter with standard response headers.                  |
| `TimeoutMiddleware`                       | Runs a handler with a context deadline and returns 504/499 when possible.        |
| `LimitBodyMiddleware`                     | Wraps request bodies with `http.MaxBytesReader`.                                 |
| `GzipMiddleware` / `WithGzip`             | Compresses eligible responses of at least 1,400 bytes.                           |
| `SecurityMiddleware`                      | HSTS, XSS, nosniff, frame, CSP, and referrer headers.                            |
| `CSRFMiddleware`                          | Uses Go cross-origin protection plus configured exact/wildcard trusted origins.  |
| `IdempotencyMiddleware`                   | Coalesces concurrent unsafe requests and optionally caches successful responses. |
| `StaticAssetsMiddleware`                  | Serves `/assets/*` from `./assets` with type-based cache TTLs.                   |
| `APIPrefixMiddleware`                     | Removes the configured prefix before routing.                                    |
| `RedirectMiddleware`                      | Redirects HTTP to HTTPS, strips `www.`, and removes trailing slashes.            |
| `RewriteMiddleware`                       | Rewrites literal or regular-expression paths before routing.                     |
| `SanitizeURLMiddleware`                   | Sanitizes paths, query parameters, and parsed POST/PUT form values.              |
| `MaliciousRequestMiddleware`              | Requires a user agent and blocks configured scanner/automation signatures.       |
| `BlockMaliciousPathsMiddleware`           | Returns 404 for common secret, backup, admin, and executable paths.              |
| `OutdatedBrowserMiddleware`               | Returns 426 for selected legacy browser signatures.                              |
| `BotAndAiDetectionMiddleware`             | Adds bot/AI flags consumed by `IsBot`, `IsAi`, and `GetClientType`.              |
| `ServerContextMiddleware`                 | Makes the server available through `GetServer` / `MustGetServer`.                |
| `CacheControlMiddleware`                  | Sets a fixed `Cache-Control` value.                                              |
| `ResponseHeader`                          | Sets a fixed response header before the endpoint handler runs.                   |

Example rewrite and endpoint-specific caching:

```go
s.Use(s.RewriteMiddleware(map[string]string{
	"/old":                  "/new",
	`^/users/(\d+)/legacy$`: `/users/$1`,
}))

cachedHandler := goserver.CacheControlMiddleware("public, max-age=300")(
	http.HandlerFunc(listHandler),
)
s.GET("/catalog", cachedHandler.ServeHTTP)
```

Trusted cross-origin values are a comma-separated list such as `https://app.example.com,https://*.example.net`. `CSRF_TRUSTED_ORIGINS=*` bypasses cross-origin checks and should only be used when that is explicitly intended.

## Errors, hooks, and background work

The default error format is HTML for GET and JSON for non-GET requests. Set `APP_ERROR_MODE=json` to always use JSON defaults. Call `RenderError` or `RenderErrorJSON` directly when a handler needs an explicit status.

```go
s.ErrorNotifier = func(status int, r *http.Request, message string) {
	// Send the already stack-enriched message to your error system.
}

goserver.RegisterBeforeRequest(func(w http.ResponseWriter, r *http.Request) {
	// Runs immediately before the compiled handler chain.
})

goserver.RegisterAfterRequest(func(w http.ResponseWriter, r *http.Request) {})
goserver.RegisterOnError(func(w http.ResponseWriter, r *http.Request, err error) {})
goserver.RegisterOnShutdown(func() {})
```

Application hooks configure a server before it starts:

```go
goserver.RegisterHookWithPriority(10, func(s *goserver.Server) {
	s.GET("/plugin", pluginHandler)
})

s.ApplyHooks()
```

Use `RunAsync` for work that must be tracked during graceful shutdown and panic-protected:

```go
s.RunAsync(func() {
	refreshSearchIndex()
})
```

`Go` is a lighter panic-logging helper for detached work; unlike `RunAsync`, it is not awaited during shutdown.

## Cookies, sessions, JWT, and encryption

### Cookies

```go
s.SetCookie(w, "theme", "dark", goserver.CookieOptions{
	MaxAge:   30 * 24 * 60 * 60,
	Secure:   true,
	HttpOnly: true,
	SameSite: http.SameSiteLaxMode,
})

cookie, err := s.GetCookie(r, "theme")
goserver.RemoveCookie(w, "theme")
goserver.RemoveAllCookies(w, r)
```

Session cookies named `session_id` are transparently encrypted with AES-GCM using `SESSION_KEY`.

### In-memory sessions

Initialize storage before serving requests:

```go
s.SetSessionStorage(goserver.NewInMemorySessionStorage(10_000))

s.POST("/login", func(w http.ResponseWriter, r *http.Request) {
	id := s.SessionStart(&w, r)
	id.SetSessionValue("user_id", 42)
	s.RespondJSON(w, r, map[string]bool{"ok": true})
})

s.GET("/me", func(w http.ResponseWriter, r *http.Request) {
	id := s.SessionStart(&w, r)
	userID := id.GetSessionValue("user_id")
	s.RespondJSON(w, r, map[string]any{"user_id": userID})
})

s.POST("/logout", func(w http.ResponseWriter, r *http.Request) {
	id := s.SessionStart(&w, r)
	id.Destroy(&w)
	w.WriteHeader(http.StatusNoContent)
})
```

`GetOrSetSessionValue[T]` coalesces concurrent generation for a missing value:

```go
profile, err := goserver.GetOrSetSessionValue(id, "profile", func() (Profile, error) {
	return loadProfile(r.Context())
})
```

The default session lifetime is 12 hours. Use Redis storage for multiple processes or persistent deployments.

### JWT

Tokens use HS256 and store user data under `data` plus an `exp` timestamp:

```go
type Claims struct {
	UserID int      `json:"user_id"`
	Roles  []string `json:"roles"`
}

token, err := goserver.GenerateToken(s.JWT, Claims{UserID: 42}, 15*time.Minute)
claims, err := goserver.ValidateToken[Claims](s.JWT, token)
```

Validation can return `ErrTokenMalformed`, `ErrTokenSignature`, or `ErrTokenExpired`. Set a stable, strong `JWT_SECRET` in production.

### String encryption and WebSocket tokens

```go
encrypted, err := goserver.EncryptString("secret value", passphrase)
plain, err := goserver.DecryptString(encrypted, passphrase)

wsToken := goserver.GenerateWSToken(secret, 5*time.Minute)
err = goserver.ValidateWSToken(wsToken, secret)
```

## Caching and idempotency

### In-memory and disk cache

```go
cache := goserver.NewCache(10_000, "./tmp/cache") // empty path disables disk cache
s.Cache = cache

type Product struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

product, err := goserver.Fetch(r.Context(), cache, "product:42", 5*time.Minute,
	func(ctx context.Context) (Product, error) {
		return loadProduct(ctx, 42)
	},
)
```

`Fetch[T]` uses cache-aside semantics and singleflight to collapse concurrent misses. `FetchSWR[T]` returns stale data after expiry and triggers one background refresh per key, even under a concurrent stale-hit burst. Both support structs, strings, and byte slices. For the primary high-load path, pass an empty data directory (`NewCache(size, "")`) so hits remain entirely in process memory. The optional disk layer is useful for restart-tolerant low/medium traffic caches, but a disk write is synchronous with the cache fill and is not intended for a latency-critical hot path.

Direct `CacheStore` operations are also available:

```go
_ = cache.Set(ctx, "key", value, time.Minute)
value, found := cache.Get(ctx, "key")
bytes, err := cache.GetOrSet(ctx, "key", time.Minute, generator)
```

### Idempotent write endpoints

`Defaults()` no longer installs `IdempotencyMiddleware`. Mount it explicitly after authentication **and authorization** on write routes. Trusted auth middleware must set `r = r.WithContext(goserver.WithIdempotencyScope(r.Context(), tenantID, userID))` on every request before calling the next handler. Missing/empty user scope with an `Idempotency-Key` is rejected with 401. Never derive the scope from unverified headers.

Assign `s.Cache` to retain successful responses for 24 hours. Without a cache, only concurrent requests within the same middleware instance are coalesced. A shared Redis cache does not make execution atomic across replicas: use a database uniqueness constraint/transactional business key for distributed mutations. This middleware is not an exactly-once guarantee after a crash or failed cache write. A cache-write failure is logged without claiming that an already completed mutation failed.

```go
orders := s.Group("/orders")
orders.Use(authenticateAndAuthorize) // sets WithIdempotencyScope
orders.Use(s.IdempotencyMiddleware)
orders.POST("/", createOrder)
```

Request keys are limited to 256 bytes; bodies use `MAX_BODY_SIZE` (1 MiB fallback); recorded responses are bounded to 1 MiB. Oversized response recording returns 500; the operation may already have executed, so keep these routes' responses bounded. Session cookies are not replayed. Scope must include all tenant/principal distinctions used by the application.

```bash
curl -A 'Mozilla/5.0' \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: order-2026-0001' \
  -d '{"sku":"ABC","quantity":1}' \
  http://localhost:8080/orders
```

The hashed cache key combines host, tenant, principal, method, path, and `Idempotency-Key`. The fingerprint includes query, content type/encoding and body; a conflicting reuse returns 409. Cached and coalesced responses include `Idempotent-Replayed: true`. Authorization must run before replay even when a cached response exists.

## PostgreSQL

The `db` package wraps pgx v5 with pool defaults, connection retries, deadlines, transaction retries, row-to-struct mapping, batching, health checks, query logging, and pool monitoring.

Configure either `DATABASE_URL` or the individual `POSTGRES_*` variables, then connect:

```go
import goserverdb "github.com/myelophone/goserver/db"

database, err := goserverdb.NewDatabase(s)
if err != nil {
	log.Fatal(err)
}
defer database.Close()

pool := database.GetPool()
```

Queries map columns to struct fields by name:

```go
type User struct {
	ID    int64  `db:"id"`
	Email string `db:"email"`
}

user, err := goserverdb.Get[User](ctx, pool,
	`SELECT id, email FROM users WHERE id=$1`, 42,
)

users, err := goserverdb.Select[User](ctx, pool,
	`SELECT id, email FROM users ORDER BY id LIMIT $1`, 100,
)

affected, err := goserverdb.Exec(ctx, pool,
	`UPDATE users SET email=$1 WHERE id=$2`, "new@example.com", 42,
)
```

Transactions retry selected serialization, deadlock, lock, and connection errors up to three times:

```go
err := goserverdb.RunInTx(ctx, s, pool, pgx.TxOptions{
	IsoLevel: goserverdb.IsoLevelSerializable,
}, func(tx pgx.Tx) error {
	if _, err := goserverdb.Exec(ctx, tx,
		`UPDATE accounts SET balance=balance-$1 WHERE id=$2`, 100, fromID,
	); err != nil {
		return err
	}
	_, err := goserverdb.Exec(ctx, tx,
		`UPDATE accounts SET balance=balance+$1 WHERE id=$2`, 100, toID,
	)
	return err
})
```

Use `SendBatch` with `pgx.Batch`, `HealthCheckDB` for a deeper read/transaction health test, `WithTimeout` to add a deadline only when one is absent, and `IsConnectionError` for coarse connection error classification.

## Redis

The import path is `github.com/myelophone/goserver/redis`; its package name is `goredis`.

### Redis cache

```go
import goredis "github.com/myelophone/goserver/redis"

s.Cache = goredis.NewCache(goredis.CacheConfig{
	Addr:         "127.0.0.1:6379",
	Password:     "",
	DB:           0,
	PoolSize:     100,
	MinIdleConns: 10,
})
```

It implements `goserver.CacheStore`, so the same `Fetch`, `FetchSWR`, and idempotency examples work unchanged. `RedisCache` additionally exposes `Delete` when retained as its concrete type.

### Redis sessions

```go
s.SetSessionStorage(goredis.NewSession(goredis.SessionConfig{
	Addr:       "127.0.0.1:6379",
	Password:   "",
	DB:         1,
	Expiration: 12 * time.Hour,
}))
```

`NewSession` verifies Redis connectivity and panics if the initial ping fails, so initialize it during process startup.

## Nuxt-like web application

`goserver` includes an optional, file-based SSR web layer for full sites and services. It is disabled by default. Enable it during server setup, before `Run()`:

```go
s := goserver.NewServer("8080")
s.Defaults()

if err := s.EnableWeb(); err != nil {
	log.Fatal(err)
}

// Explicit goserver routes retain priority over file-based pages.
s.GET("/api/health", health)
s.Run()
```

`EnableWeb` installs the application only as the router fallback, so existing routes, middleware, operational endpoints, WebSockets, sessions, cache, database integrations and response helpers remain available. A rendering failure uses goserver's embedded `error.html` through `RenderError`; a file-based `404.gosh` remains available for not-found pages.

The web source is a layered filesystem: goserver provides the base layer, and the importing application's files override matching relative paths. This works for any consuming Go module, not only `goserver-template`. A consumer only needs to create the files it wants to add or replace:

```text
websettings.json             # complete, non-secret web-framework defaults
websettings.Development.json # optional local Development overrides
websettings.Production.json  # optional local Production overrides
web/
  pages/                 # index.gosh, users/[id].gosh, docs/[...all].gosh
  components/            # reusable GOSH SFCs
  layouts/               # layouts; default.gosh is selected by config
  global/{head,styles,scripts}/
  logic/                 # optional page/component Go logic
  modules/               # local Go modules require explicit imports
  plugins/               # optional client runtime-hook JavaScript
  server/                # optional file-based HTTP handlers (*.method.go)
  stores/
  content/               # Markdown posts available at /post/<filename>
  system/                # optional overrides; inherited from goserver by default
assets/                  # optional local public files and base-file overrides
internal/goservergen/     # generated automatically; do not commit
cmd/web_import_gen.go    # generated automatically; do not commit
```

Pages follow Nuxt-style routing: `index.gosh` maps to its directory, `[id].gosh` is a parameter, and `[...all].gosh` is a catch-all. Components support SSR, layouts, scoped CSS, client/lazy components, streaming runtime navigation, server actions, content-addressed JS/CSS assets, route/component SWR rules, SEO composables, and hooks. The inherited home page and welcome component are demonstration defaults, intended to be replaced by the application. Server logic is optional and is referenced from a page or component with its `@server path#Export` directive; ordinary server endpoints remain explicit goserver routes outside `web/`.

`go run -tags webcli ./cmd generate` scans the merged pages, components, and layouts for `@server` directives. It writes compile-time bindings under `internal/goservergen/`, never into editable `web/logic`; only exports referenced by GOSH files are registered. It also generates `cmd/web_import_gen.go` to link these bindings into the application. Web-enabled Task workflows run generation before compilation. File-based page paths come from the merged `web/pages` layer when `EnableWeb()` starts.

The web framework is opt-in: set `runtime.enabled` to `true` in `websettings.json`, or set `MYELOPHONE_WEB_ENABLED=true`. Without either, `cmd/main.go` starts only the regular goserver application. The repository's Taskfile selects web generation and web builds using `MYELOPHONE_WEB_ENABLED=true`; set that variable for web-enabled Task workflows, or invoke `task web:generate` / `task web:build` explicitly. Development web tooling uses `go run -tags webcli ./cmd <setup|generate|build|audit|clean|prune-unused --yes>`. `audit` is report-only and checks `assets/`: it reports files with no static reference in the layered sources. Root `favicon.ico`, `favicon.png`, and `favicon.svg`, as well as `build.includeFiles`, are delivery assets and are excluded from that report. Only `robots.txt`, when present in the resolved public layer, is embedded in the production binary. `clean` is optional and removes `dist/`, generated bindings/imports, legacy build output and audit reports; it does not remove application source or the shared toolchain cache. The CLI's build-tagged entry point is excluded from normal and production binaries.

`web/server` is optional. A file named `web/server/pogoda.get.go` maps to `GET /pogoda`; `web/server/api/pogoda.get.go` maps to `GET /api/pogoda`. Nested folders become URL segments and are imported as separate Go packages automatically. Dynamic segments belong in the file name (`web/server/users/[id].get.go`), since a Go import path itself cannot contain `[` or `]`. It exports one handler with this public contract:

```go
func Pogoda(event *runtime.Event) error {
    city := event.Query("city")
    requestID := event.Context().Value(requestIDKey)
    return event.JSON(map[string]any{"city": city, "requestId": requestID})
}
```

`Event` exposes `Request`, `Context()`, `Param()`, `Query()`, `Header()`, `Status()`, `JSON()`, `Text()`, and `HTML()`. These generated endpoints are fallback routes: an explicit `s.GET`, `s.POST`, and so on always has priority.

### Base layer and consumer overrides

Production and development are separated at compile time. The web builder compiles the final application with `-tags myelophone_prod`; the plain-server Task build uses the same tag. Production excludes the complete development layer, playground, embedded Yarn/tooling configuration, installers, CSS/JS builders, binding generation, and web CLI. It consumes the resolved application bundle and delivered disk assets. Setting `APP_ENV=prod` on an ordinary untagged Go build does not exclude development code.

This also applies to importing applications: the dependency supplies development/build resources without creating a local `web/system` tree, while the consumer's production binary contains only the runtime and prepared application resources. Use a goserver version containing this separation and the production build workflow; do not copy tooling into the consuming repository or final image.

Layers merge by relative file path, not by copying the entire base tree into the consumer. For example, an application's `web/pages/index.gosh` replaces the base home page; `web/pages/about.gosh` adds a page; removing the local `index.gosh` restores the base home page. A missing or empty local `web/pages` directory still inherits the base pages, including the home page, 404 and error pages. Components, layouts, global inserts, CSS, stores, plugins, content, teleports, system templates/runtime and file-based server handlers follow the same file-level rule. File contents are replaced, not concatenated; CSS files participate in the normal cascade described below.

Consumers do not need a duplicate `web/system` tree, a generated-package stub, or empty Go package placeholders. In particular:

- `web/server/server.go` containing only `package server` can be omitted. Add actual `*.method.go` handlers when needed; a same-path local handler replaces the base handler, while a new path adds a route.
- `web/logic/logic.go` is only a convenience alias facade. It can be omitted when no local code imports that package or uses its aliases. Local handlers can import `github.com/myelophone/goserver/web/runtime` directly.
- An empty `web/modules/modules.go` can be omitted when not imported. Local modules are not automatically discovered: explicitly import the local module package from application code so its registration runs. The framework imports its own base modules.

Go packages themselves are not merged. For a referenced handler, generation imports the consumer package if that referenced Go file exists locally; otherwise it imports the goserver package. A local replacement must therefore compile as a normal, complete Go package and explicitly import any base functionality it reuses. Regenerate bindings after adding or removing handlers; the web-enabled `task run`, `task dev` and `task build` workflows do this automatically.

The welcome page shows the goserver dependency version from Go build information, not the consuming application's version. When goserver itself is the main module, its main-module version is used, falling back to the VCS revision or `dev` for development builds. A development replacement is not equivalent to a published release.

Use a goserver dependency revision that includes this layer implementation. Updating the library's working tree does not update a consumer pinned to an older release. No permanent workspace, `replace`, module-cache edits or consumer-specific overlay are required for normal use.

`websettings.json` contains every non-secret web-framework setting and its defaults. It deliberately has no listener address or port: the web layer never starts a second HTTP server. Configure the listener once when creating `goserver.NewServer(...)` (normally from `HTTP_PORT`), then attach web with `EnableWeb()`. At startup goserver applies built-in defaults, then the consuming project's local `websettings.json`, then `websettings.{Environment}.json`. `Environment` comes from `APP_ENV`: Task targets map `dev` to `Development` and `prod` to `Production`. Thus a project that imports goserver can keep its own settings files alongside its own `go.mod`; they override framework defaults without modifying the dependency. Environment variables remain the final override layer. `render.serverTiming` is disabled by default; set it to `true` only for profiling builds. `MYELOPHONE_WEB_RENDER_SERVER_TIMING` overrides that setting, and `task web:build:profile` creates a production web build with the header enabled.

Use `build.includeFiles` for asset paths (relative to `assets/`) which must be copied to `dist/assets` even without a static source reference. For example, `"build": { "includeFiles": ["downloads/catalog.pdf"] }`. Requests for a root path such as `/favicon.ico` or `/downloads/catalog.pdf` are checked in `assets/` before the web handler returns a 404. Production embeds `websettings.json`, its environment override, and `assets/robots.txt`, so those files are not copied beside the executable.

### Cookie consent

Cookie consent uses the same top-level configuration shape as MyelophOne/Nuxt. `cookieControl.enabled` is the hard switch. When it is `false`, the default layout does not render the banner/config/settings markup, their component CSS and route JavaScript are excluded, and the `cookies` store is omitted from the shared client entry. `autoMount: false` keeps consent handling available for manually placed components while disabling the layout-provided UI.

```json
{
 "cookieControl": {
  "enabled": true,
  "autoMount": true,
  "cookieName": "privacy-preferences",
  "maxAgeDays": 365,
  "declineReaskDays": 30,
  "banner": { "enabled": true, "position": "bottom-left" },
  "settings": { "enabled": true, "showCookieList": true }
 },
 "cookieScripts": {
  "necessary": [],
  "analytics": [
   {
    "id": "analytics",
    "name": "Analytics",
    "description": "Anonymous traffic statistics",
    "loadKey": "analytics",
    "src": "https://analytics.example/script.js",
    "attributes": { "defer": "true" },
    "legalBasis": "consent",
    "legal": { "privacyPolicyUrl": "https://example.com/privacy" }
   }
  ],
  "marketing": [],
  "functional": [],
  "notices": []
 }
}
```

Configured `src` or inline `code` is loaded only after its category is allowed. Definitions may also use `initializers`, `beforeLoad`, `onConsentChange`, and per-category `categorySettings`; `loadKey` deduplicates a vendor shared by several categories. Cookie script configuration is trusted application configuration and must never contain user input.

The default layout mounts `CookieBanner` and `CookieSettingsModal`. Any element with `data-cookie-open-settings` opens the settings again. Use the content components directly in GOSH templates:

```html
<ConsentYoutube video-id="dQw4w9WgXcQ" />
<ConsentGoogleMap address="Warsaw, Poland" language="pl" region="pl" />

<CookieConsentWrapper category="marketing" service-name="Example video">
 <iframe src="https://example.com/embed/123"></iframe>
</CookieConsentWrapper>

<CookiePrivacyPolicy title="Cookie policy" :show-sources="true" />
<button type="button" data-cookie-open-settings>Cookie settings</button>
```

Consent-wrapped markup is emitted inside an inert `noscript` container and is instantiated only after permission, so an iframe cannot contact its provider before consent. Preferences use a first-party `SameSite=Lax` cookie and synchronize across open tabs.

`values` is deliberately schema-free: it accepts any valid JSON value without a second schema or generated type file. Strings, booleans, numbers, arrays, objects, and `null` retain their JSON types in `RuntimeConfig.Values`. Use `cfg.String("key")`, `cfg.Bool("key")`, or `cfg.Int("key")` for scalar convenience; retrieve nested arrays and objects from `cfg.Values["key"]` when needed. Framework-owned sections remain typed and validated separately.

### Zustand store hydration

`logic.UseStoreState` provides Nuxt-style store hydration without base64. During SSR it collects serializable store snapshots and goserver emits one safe `<script type="application/json" id="gosh-store-state">` block before the application. The runtime applies that JSON to registered Zustand stores before page and component modules mount. Never put secrets in a store snapshot.

```go
func (CatalogPage) Render(ctx *logic.Context, props logic.Props) (logic.Data, error) {
    logic.UseStoreState(ctx, "cart", map[string]any{
        "count":        3,
        "lastProductId": "keyboard",
    })
    return logic.Data{}, nil
}
```

Inside a `.gosh` client script, use the matching `_gosh.useStoreState(name, state)` composable. It merges serializable fields into a registered store and also retains the snapshot for a store that registers later:

```html
<script>
 export function mount() {
  _gosh.useStoreState("cart", { drawerOpen: true });
 }
</script>
```

Use server hydration for request-specific initial values. Use the GOSH composable for client-side changes. A store may later persist selected fields through its session configuration without changing either API.

Every `web/stores/*.js` module is bundled into the one global runtime entry and registered before page or layout modules mount. A store may export `setup({ name, store, gosh })`; use it for browser listeners and subscriptions, and return a cleanup function. This keeps store lifecycle independent of the selected layout:

```js
export function setup({ store }) {
 const unsubscribe = store.subscribe((state) =>
  localStorage.setItem("theme", state.theme),
 );
 const onStorage = (event) => {
  /* apply cross-tab updates */
 };
 window.addEventListener("storage", onStorage);
 return () => {
  unsubscribe();
  window.removeEventListener("storage", onStorage);
 };
}
```

Templates can use a store without a component script: `data-gosh-store-text="cart.count"` renders and keeps a text node in sync, while `data-gosh-store-action="preferences.toggleTheme"` invokes an action on click. Pass action parameters as JSON with `data-gosh-store-args='["dark"]'`.

### GOSH file standard

`.gosh` is the native **GOServer Hybrid** format. It is the framework’s template-file extension. A renderable page, layout, or component is a UTF-8 single-file component (SFC) with one `<template>` block that contains its HTML. It may additionally contain CSS (global or scoped), JavaScript, and HTML comments. The framework preserves comments inside `<template>` and recognizes comment directives outside it.

```html
<!-- @layout default -->
<!-- @server ./web/logic/catalog.go#CatalogPage -->

<template>
 <!-- Visible only in the rendered HTML source. -->
 <main class="catalog"><ProductCard /></main>
</template>

<style>
 :root {
  --catalog-gap: 1rem;
 } /* Global CSS */
</style>

<style scoped>
 .catalog {
  display: grid;
  gap: var(--catalog-gap);
 }
</style>

<script setup>
 const title = "Catalog";
</script>

<script>
 _gosh.hook("page:afterNavigate", ({ url }) => console.info(url));
</script>
```

The standard is deliberately strict:

- A page, layout, or component has exactly one `<template>` block; HTML belongs only inside that block. `<head>` is optional and is appended to the document head.
- Use `<style scoped>` for styles owned by that SFC; goserver assigns a stable scope attribute to its rendered elements. Use plain `<style>` for intentional global CSS. Multiple style blocks are allowed.
- `<script setup>` is allowed once and runs as setup code. Ordinary `<script>` blocks are allowed more than once and are emitted as client code. A `.server.gosh` component must not contain ordinary `<script>` blocks; `.client.gosh` marks a client component, and a name without either suffix is universal.
- Use standard `<!-- ... -->` comments. `<!-- @layout name -->` selects a page layout (`none` disables it); `<!-- @server path#Export -->` binds a Go handler. Keep directives at the beginning of the file and one directive per comment for reviewability.
- The browser runtime namespace is `_gosh` (or `window._gosh`). Framework-owned requests use `/_gosh/`, and generated DOM hooks use `data-gosh-*`; applications must not depend on their exact markup beyond documented runtime APIs.
- SSR props are never serialized into HTML—not as base64, JSON, or an inline script. The rendered DOM contains only opaque `data-gosh-state` continuation tokens for server actions and lazy islands. Tokens are random, bound to an HttpOnly same-site cookie, expire after 30 minutes, and are held in a bounded server store (4,096 entries per process); an expired token reloads the document safely. This keeps rendered source clean and prevents a client from modifying server props. Keep intentional public client state in Zustand via `logic.UseStoreState`; keep sensitive or authoritative state in goserver sessions. Client modules receive `ctx.props = {}` by design: pass required public values through a store or explicit module data instead. For a multi-instance deployment, register a shared owner-bound implementation with `app.SetRuntimeStateStore(...)` during setup; the default in-memory store is appropriate for a single instance or sticky sessions.
- `web/global/head/*.gosh`, `web/global/scripts/*.gosh`, and `web/global/styles/*.gosh` are injection fragments rather than renderable SFCs: they contain, respectively, trusted head markup, script markup, or raw CSS. Use `.gosh` for their extension as well.

Each page can select its own layout at the top of its `.gosh` file. `render.defaultLayout` is used only when the page has no directive:

```html
<!-- @layout marketing -->
<template>...</template>
```

Use `<!-- @layout none -->` for a page without a layout. Layout files live in `web/layouts/`; `web/pages/index.gosh` is the working `default` layout example.

Missing optional template values render as an empty value instead of turning the page into a 500 response. A failing component, layout handler, or individual node is isolated so remaining SSR markup can still be sent; the failure is logged and exposed to templates as `renderError`. Invalid source files and startup configuration remain startup errors.

When neither a page nor its server logic specifies SEO, the built-in defaults are `Our Nuxt WebSite | by MyelophOne/GoServer` and `Welcome to our new amazing website where you can explore exciting content and features! Empowered by MyelophOne`. Override them with the `seo` object in `websettings.json`.

All website paths are canonicalized without a trailing slash: `/catalog/` permanently redirects to `/catalog`. Use `goserver.CanonicalURL` when producing application links from Go.

Markdown files in `web/content/` are published as posts. `web/content/hello.md` is served at `/post/hello`; nested files keep their nested path. An optional frontmatter block supplies page SEO:

```md
---
title: "Post title"
description: "Page description"
image: "/assets/posts/example.jpg"
---

# Post body
```

Set `content.layout` in `websettings.json` to choose the layout for all Markdown posts. When omitted, posts use `render.defaultLayout`; set it to `none` for raw post markup. `MYELOPHONE_WEB_CONTENT_LAYOUT` is the equivalent environment override.

Files stored next to Markdown are content resources, not embedded source. Production builds copy them to `dist/assets/content/` with their relative path preserved, so `web/content/guides/intro/cover.jpg` is served as `/assets/content/guides/intro/cover.jpg`. A relative frontmatter value such as `image: "cover.jpg"` is resolved automatically from its Markdown directory; use `/assets/...` or an absolute URL to leave a value untouched. This is useful for frontmatter SEO images, Markdown media, downloadable files, and fonts. Tenant content follows the equivalent `/assets/tenants/<tenant-id>/content/...` path. JPEG and PNG content resources use the same optional production optimization as `assets/`.

### Tenant-specific pages and content

When the existing `TenantStore` middleware has resolved a tenant, the web layer checks `web/tenants/<tenant-id>/pages/` before falling back to `web/pages/`, and checks `web/tenants/<tenant-id>/content/` before `web/content/`. This lets a tenant override only the pages or Markdown posts it owns; it does not require a duplicate site tree. Tenant IDs are treated as directory names and must not contain path separators.

```text
web/tenants/
  tenant3/
    pages/
      index.gosh             # overrides / for tenant3
      pricing.gosh           # adds /pricing only for tenant3
    content/
      welcome.md             # overrides /post/welcome for tenant3
```

Register the tenants and middleware before `EnableWeb`:

```go
tenantStore := goserver.NewTenantStore()
tenantStore.AddTenant(&goserver.Tenant{
	ID: "tenant3", Name: "Tenant Three",
	Config: map[string]any{"domain": "*.example.com"},
})
tenantStore.AddTenant(&goserver.Tenant{
	ID: "tenant4", Name: "Tenant Four",
	Config: map[string]any{"domain": "*.sub.example.com"},
})

s.Use(tenantStore.MiddlewareByDomain())
if err := s.EnableWeb(); err != nil { log.Fatal(err) }
```

Tenant page/content trees are loaded once on first request for that tenant and concurrent first requests share the same load. Page and file-based API cache keys include the resolved tenant ID.

Set `locales` and `defaultLocale` in `websettings.json` to expose every file-based page under non-default locale prefixes. For example, `"locales": ["en", "ru", "pl"]` with `"defaultLocale": "en"` serves the home page at `/`, `/ru`, and `/pl`; `/about` also becomes `/ru/about` and `/pl/about`. Each localized file-based page automatically emits `hreflang` links for all configured languages plus `x-default`; 404, error, `/post/*`, and explicit physical language routes are excluded. `MYELOPHONE_WEB_LOCALES=en,ru,pl` and `MYELOPHONE_WEB_DEFAULT_LOCALE=en` provide the equivalent deployment-time overrides. The `/post/` namespace and not-found pages are deliberately not locale-prefixed. The active locale is available to page logic and templates as `locale` and `lang`.

`web/pages/i18n.gosh` is a complete I18n example. Initialize `s.NewI18n("en", []string{"en", "ru", "pl"})` before `EnableWeb`, then open `/i18n`, `/ru/i18n`, or `/pl/i18n`. Its language links are ordinary internal links, so `window._gosh` turns them into SPA navigation while the server re-renders the translated page. In web server logic call `ctx.T("common.web.heading")` to use the existing goserver I18n messages.

Use the regular application entrypoint and the build-tagged CLI. With `MYELOPHONE_WEB_ENABLED=true` set for the Taskfile:

```bash
task setup
task run
task dev
task build
task server
task web:audit
task web:clean
```

`task setup` installs the builders; `task run` starts development, `task dev` adds Air live reload, `task build` creates the production distribution, and `task server` runs the built executable. Setup also runs automatically before `web:generate` and `web:build`. `setup:client` and `setup:tailwind` are compatibility aliases for the same setup. `web:audit` only reports unused public assets, while `web:clean` removes generated output. `task preview` runs source with production settings; it is not a substitute for testing the built distribution.

Applications can use `goserver.RunWebCLI(os.Args[1:])` in a `cmd/web_cli.go` entrypoint tagged `webcli`, with the regular main entrypoint tagged `!webcli`. Invoke it with `go run -tags webcli ./cmd setup`, `generate`, or `build`. Always run/build the entire `./cmd` package, not `./cmd/main.go`: single-file compilation omits the generated importer. The generated importer is excluded from CLI builds, allowing generation from a clean checkout or after `web:clean`, without a committed stub.

Production resources belong to the importing application. The production build generates reachable embedded sources, compiled CSS, client bundles and resolved configuration under `internal/goservergen`. It invokes normal Go compilation of `./cmd` with the production build tag and registers the resulting resources through `web/runtime.ProductionBundle`. The library's production helpers are always available; importing goserver does not require production-generated symbols inside the dependency. Temporary production source/config/embed files are removed after the build, while handler bindings and the generated importer remain for subsequent development. The goserver dependency and Go module cache are never modified. The resulting binary starts without project source files, Go, Node.js or Tailwind installed; public files still require the deployed `assets/` directory. This contract works with any application module path, including applications that do not use goserver-template.

Generated files must not be committed. Add these entries to the consuming project's `.gitignore` and exclude them from Docker build contexts and live-reload inputs:

```gitignore
internal/goservergen/
cmd/web_import_gen.go
```

The web layer is inherited from goserver. Pages, components, layouts, global inserts, CSS, stores, plugins, teleports, templates, content and public assets are merged with the importing application's files. A local file overrides the base file at the same relative path; removing the local override restores the base file. Generated Go bindings import a local handler package when the referenced Go file exists in the application, otherwise they import the base goserver package. JavaScript compilation uses temporary merged inputs under `tmp/goserver/build` that are removed after compilation. Projects do not need `web/system`: `task setup` installs the embedded, lockfile-pinned esbuild, Tailwind and PostCSS toolchains in the user cache. The default cache is `os.UserCacheDir()/myelophone/goserver/toolchains/<fingerprint>`; the fingerprint includes tool configuration, OS and architecture. Set `MYELOPHONE_TOOLCHAIN_CACHE` to choose another cache root. Optional local toolchain configuration overrides use a separate cache entry; lockfiles are installed in immutable mode. Existing installations are reused, with locking to coordinate concurrent setup. Node.js is required for setup, development and compilation, but not for running the production binary. Keep the base `web/system` files in the goserver library itself: they supply the inherited resources and embedded tool definitions.

Public assets are not embedded in the binary. Development resolves the goserver module directory with `go list -m -json` and reads its base assets from disk, with application files overriding the same relative paths. This uses the actually selected dependency, including development replacements/workspaces if explicitly configured. Production builds merge the selected public layers into `dist/assets`, applying the configured unused-file filtering and image optimization. Deploy the executable together with that directory. Relative production public asset roots resolve beside the executable, independent of the process working directory. Only the resolved `robots.txt`, when present, is embedded; a local `assets/robots.txt` overrides the base version. Other public files are served from disk, including files with application-specific names and paths selected through web settings. Changing a disk asset changes what is served; removing it produces a 404 rather than falling back to an embedded image. Files addressed dynamically must be listed in `build.includeFiles` when they have no detectable static reference.

When web support is enabled, `task build` writes `tmp/web-assets.json`: the immutable final JS/CSS URLs with raw and gzip sizes. Build extensions can observe the same manifest in every environment with `logic.HookBuildAssetsBefore` and `logic.HookBuildAssetsAfter`; the latter receives `[]logic.BuildAsset`.

Set `render.spaLoadingTemplate` to `true` to insert `web/system/templates/spa-loading-template.html` before `#app`. It remains visible while the deferred runtime is loading and is removed immediately after `window._gosh.start()` succeeds. The option is disabled by default and can also be set with `MYELOPHONE_WEB_RENDER_SPA_LOADING_TEMPLATE=true`.

Tailwind CSS v4 is required by the web layer. `task setup` installs the cached builders and is also invoked automatically by the web generation/build tasks; `tailwind.minify` controls only minification. Production freezes the compiled styles and their page bindings during the build, so starting the production executable does not re-run Tailwind or PostCSS. The compiler builds Tailwind once for the full reachable graph and emits it as a content-addressed common asset. With `render.splitCss=true`, every response loads that common CSS separately and then only its page/component CSS; both are split at `render.cssMaxChunkSize`. `render.cssMinChunkSize` rebalances CSS rules between the final chunks when that keeps every chunk within the configured maximum; a small single page asset remains separate so the common CSS stays cacheable. A single CSS rule larger than the maximum is emitted intact. With `render.splitCss=false`, startup builds exactly one immutable stylesheet from Tailwind plus every known page, layout, component, and lazy-component style. Every route receives the same URL and lazy streaming never adds another CSS file.

The `Generated assets` section of `task build` lists only immutable files created during the production build: the shared Tailwind CSS, browser entry, WebSocket chunk, and optional Web Vitals chunk. A shared CSS file smaller than `render.cssMaxChunkSize` remains one file even with `render.splitCss=true`. Route-specific component/page CSS is emitted on demand as `/_gosh/style/<hash>.css`. Likewise, page/component client scripts are route-scoped and emitted on demand as `/_gosh/chunk/<hash>.js`; they are not pre-listed by the build output. The browser runtime API is `window._gosh` (and `_gosh` in client scripts); it exposes hooks, stores, navigation, and SEO helpers. Environment variables override configuration files: `MYELOPHONE_WEB_RUNTIME_CACHE_TTL`, `MYELOPHONE_WEB_RUNTIME_WEB_VITALS`, `MYELOPHONE_WEB_RUNTIME_PREFETCH_DELAY`, `MYELOPHONE_WEB_RUNTIME_PREFETCH_MAX_CONCURRENT`, `MYELOPHONE_WEB_RENDER_DEFAULT_LAYOUT`, `MYELOPHONE_WEB_CONTENT_LAYOUT`, `MYELOPHONE_WEB_LOCALES`, `MYELOPHONE_WEB_DEFAULT_LOCALE`, `MYELOPHONE_WEB_RENDER_EARLY_HINTS`, `MYELOPHONE_WEB_RENDER_PRELOAD_RUNTIME`, `MYELOPHONE_WEB_RENDER_PRELOAD_PAGE_STYLES`, `MYELOPHONE_WEB_RENDER_SPLIT_CSS`, `MYELOPHONE_WEB_RENDER_CSS_MIN_CHUNK_SIZE`, `MYELOPHONE_WEB_RENDER_CSS_MAX_CHUNK_SIZE`, `MYELOPHONE_WEB_RENDER_SPA_LOADING_TEMPLATE`, `MYELOPHONE_WEB_TAILWIND_MINIFY`, `MYELOPHONE_WEB_IMAGES_OPTIMIZE`, and `MYELOPHONE_WEB_IMAGES_JPEG_QUALITY`.

### Runtime performance, islands, and errors

The runtime indexes managed `<meta>`, `<link>`, page CSS, fragment CSS, and runtime styles once rather than repeatedly scanning the document head during navigation. Consecutive patch operations are applied as one DOM batch. It keeps full SSR/native navigation when the browser lacks a required modern primitive (`Promise`, `fetch`, `URL`, `AbortController`, `Map`, or `Set`); no interceptor is installed in that case.

### Viewport reveal animation

Use `data-gosh-reveal` on a page or component element to apply the existing reveal CSS when it approaches the viewport. The runtime uses one shared `IntersectionObserver`, batches simultaneous entries for 50ms, and adds classes in `requestAnimationFrame`; it never listens to scroll events. Reveal runs once by default. Add `data-gosh-reveal-repeat` to reset the element after it leaves the viewport. `data-gosh-reveal-step` controls the stagger in milliseconds, and `data-gosh-reveal-speed="fast"` or `"slow"` selects the existing speed variants.

```html
<section data-gosh-reveal="slide" data-gosh-reveal-step="120">
 <h2>Appears once</h2>
</section>
<article
 data-gosh-reveal="slide-left"
 data-gosh-reveal-repeat
 data-gosh-reveal-speed="fast"
>
 Appears again after re-entering the viewport.
</article>
```

Reveal classes are emitted during SSR, so a JavaScript-enabled document never paints an element and then hides it before the observer runs. Reveal never uses `display:none` or removes content from SSR HTML: text, links, and semantic markup remain in the initial response and layout. The SSR document remains visible without JavaScript. The runtime immediately shows reveal elements when the browser lacks `IntersectionObserver`, the visitor prefers reduced motion, or the connection has Save-Data/2G enabled.

Client components are islands. A `.client.gosh` component defaults to `hydrate="visible"`, which starts its server lazy-render shortly before it enters the viewport. Choose the priority on the component call site:

```html
<SearchPanel hydrate="visible" />
<AnalyticsPanel hydrate="idle" />
<LoginDialog hydrate="interaction" />
<CriticalCart hydrate="immediate" />
```

Wrap any component tree in the system `<ClientOnly>` tag to defer it until after the initial page render. It emits the normal loader first, then requests each wrapped component through the lazy endpoint; its `@server` logic runs at that deferred render just as it does for a `.client.gosh` island.

```html
<ClientOnly>
 <AccountPanel userId="42" />
 <Recommendations />
</ClientOnly>
```

`visible` uses `IntersectionObserver` with a 240px margin; browsers without it safely load the island immediately. `idle` uses `requestIdleCallback` when available, `interaction` waits for pointer/focus, and `immediate` preserves eager behavior. A failed navigation, action, or island emits a `runtime:*error` event and renders an accessible retry control in the affected boundary. Navigation and stream boundaries also receive `aria-busy` while loading. Applications can observe `runtime:runtime-error`, `runtime:action-error`, and `runtime:lazy-error` or register `_gosh.hook("app:error", handler)` for a custom UI.

Every document also ends with the global `#gosh-preloader` spinner. It is hidden while the root has `.nojs`; after JavaScript activates it displays during initial runtime boot, SPA navigation, server actions, forms, and lazy-island requests. The runtime tracks these through one reference-counted loading store, so overlapping requests cannot hide it early. Subscribe with `_gosh.useLoading(listener)`; `listener` receives `{ isLoading, count }`. For an application-owned asynchronous task, wrap it with `_gosh.withLoading(() => fetch(...))` rather than manually changing DOM classes. The runtime also emits `runtime:loading` and invokes `_gosh.hook("loading:change", handler)`.

Hover prefetch remains opt-in with `data-prefetch="hover"`. It waits `runtime.prefetchDelay` milliseconds (65 by default), cancels when the pointer leaves, deduplicates requests, allows at most `runtime.prefetchMaxConcurrent` requests (2 by default), and avoids `Save-Data` and 2G connections.

Set `runtime.webVitals` to `true` to dynamically import the separate, dependency-free Core Web Vitals collector. It writes one `[GOSH Web Vitals] TTFB=… | FCP=… | LCP=… | CLS=… | INP=…` development snapshot after three seconds (or sooner if the document unloads), instead of cluttering DevTools with separate lines. `INP=unavailable` means the visitor has not interacted yet or the browser does not expose the API; final LCP, CLS, and INP still arrive through the event API when the document becomes hidden. It also emits `runtime:web-vital` and calls `_gosh.hook("web-vital", handler)` for each metric. This chunk is not requested when the option is false.

```json
{
 "runtime": {
  "webVitals": true,
  "prefetchDelay": 80,
  "prefetchMaxConcurrent": 2
 }
}
```

### Server cache tags and optimistic actions

Route rules retain their existing `cache.maxAge` and `swr` behavior. Web rendering always retains its fast in-process L1 route cache. When `s.Cache` is configured before `EnableWeb`, the same existing `CacheStore` becomes a shared L2 cache for public SSR route renders; this lets cache hits and tag invalidation work across processes or instances without requiring a second cache implementation. A cache-store failure or unavailable entry simply falls back to the normal L1/render path.

For public HTML routes, the L1 cache also stores a minified full-document template. On a hit goserver substitutes a newly generated CSP nonce and visitor-bound runtime tokens instead of executing the base template or minifying HTML again. These document templates remain local to the process, are capped at 32 MiB total and 1 MiB per route, and are discarded with the corresponding route entry; L2 continues to store the portable render result.

For HTTP benchmarks and clients that use `Connection: close`, leave `render.earlyHints` disabled (the supplied `websettings.json` does so). A `103 Early Hints` response is an additional informational HTTP response before the final `200`; it is useful only when an HTTP-aware browser/proxy uses the preload links. goserver also suppresses it automatically for requests that ask to close the connection.

Enable SWR explicitly for every public route that is safe to share between visitors. For example, the root page below is fresh for 10 seconds and may be served stale for a further 10 seconds while one request revalidates it in the background:

```json
{
 "routeRules": {
  "/": { "swr": 10 }
 }
}
```

An absent matching rule intentionally returns `X-Myelophone-Cache: bypass`; that is a security guard, not a cache failure. A public cache hit returns `X-Myelophone-Cache: hit`. Requests with `Authorization` or any cookie also bypass this shared route cache, preventing identity-dependent HTML from being shared accidentally. Check the active route rule before benchmarking:

```powershell
curl.exe -s -A "Mozilla/5.0" -D - -o NUL http://localhost:8080 | Select-String X-Myelophone-Cache
```

#### Fully public static routes

Set `publicStatic: true` only for a page whose output is identical for every visitor. It is intended for marketing pages, documentation, campaign landing pages, and other content that does not need hydration, server actions, lazy islands, SPA navigation, runtime stores, or per-visitor props:

```json
{
 "routeRules": {
  "/pricing": {
   "publicStatic": true,
   "cache": { "maxAge": 60 },
   "swr": 300
  }
 }
}
```

A public-static document contains no goserver runtime script, runtime state token, or runtime-owner cookie. Its response is stable across visitors and receives a cacheable header such as `Cache-Control: public, max-age=60, s-maxage=60, stale-while-revalidate=300`, so a browser, CDN, or reverse proxy can cache it safely. Its CSP blocks scripts (`script-src 'none'`); do not use this mode for pages that require application or inline JavaScript.

`publicStatic` is an explicit author security declaration: normal cookies do not bypass its route cache, because a CDN cannot share a response while varying it by every visitor cookie. Requests carrying `Authorization` still bypass the cache. Never enable it for a page that reads cookies, sessions, request identity, authorization, experiments, geographic personalization, or any other visitor-specific value.

#### Route-rule exclusions and overrides

More specific route patterns win. You may also exclude paths from a broad rule; an excluded request falls back to the next matching less-specific rule, or bypasses caching when none remains. Use an exact path for one URL and `*fragment*` when the URL path must contain a fragment:

```json
{
 "routeRules": {
  "/**": { "cache": { "maxAge": 5 } },
  "/products/*": {
   "cache": { "maxAge": 25 },
   "swr": 25,
   "exclude": ["/products/15", "*preview*"]
  },
  "/products/15": { "cache": { "maxAge": 120 }, "swr": 60 }
 }
}
```

Here `/products/9` uses the 25-second rule, `/products/15` uses its exact 120-second override, and `/products/preview/9` falls back to the 5-second `/**` rule. Existing `*` segment patterns and `/**` prefix patterns continue to work.

The per-process L1 cache remains the hot path for both normal SWR and public-static routes. An optional shared `CacheStore`/Redis L2 stores only portable render results and tag versions; it does not serve a full document on each hit, so a Redis round trip never replaces the in-memory document-cache hot path. Each process reconstructs its local document template after an L2 hit.

On a local Windows loopback benchmark with a warmed root route, `bombardier -c 100 -d 15s -l http://localhost:8080` reached 9,632 successful `2xx` responses/s with P50 10.36 ms and P99 14.03 ms. This is an illustrative result only: compare production deployments using equal response bodies, compression, HTTP version, middleware, CPU limits and cache warmth. Do not add `103` responses into a page-RPS comparison; a `103` and the final `200` are two HTTP status lines for one document request.

```go
s := goserver.NewServer("8080")
s.Cache = goserver.NewCache(10_000, "./data/cache") // or any CacheStore, including RedisCache
if err := s.EnableWeb(); err != nil { /* handle error */ }
```

Server logic may associate a public SSR render with cache tags; a successful action can invalidate those tags. This removes affected local route-cache entries, invalidates their shared L2 counterparts by a versioned tag, and sends an invalidation frame that removes matching browser SPA-cache entries. Never tag pages that depend on identity: goserver already bypasses route caching for cookies and `Authorization`.

```go
func (ProductPage) Render(ctx *logic.Context, props logic.Props) (logic.Data, error) {
    id := props.String("id")
    logic.UseCacheTags(ctx, "product:"+id, "catalog")
    return logic.Data{}, nil
}

func (ProductPage) Action(ctx *logic.Context, action string, props logic.Props) (logic.Data, bool, error) {
    if action == "save" {
        // persist the product first
        logic.RevalidateTags(ctx, "product:"+props.String("id"), "catalog")
        return logic.Data{}, true, nil
    }
    return nil, false, nil
}
```

For client-side optimistic UI, use the same server action protocol and provide a reversible update. `apply` runs before the request; `rollback` runs only if it fails. `_gosh.optimisticWrite` is a convenient query-cache rollback source.

```js
const undo = _gosh.optimisticWrite(["product", id], (current) => ({
 ...current,
 liked: true,
}));
await _gosh.runAction("like", rootElement, {
 optimistic: { apply() {}, rollback: undo },
});
```

### Runtime forms without endpoint actions

For a GOSH server-action form, do not use HTML `action`. Name the logical server event with `data-gosh-form`; the browser posts only to the framework endpoint `/_gosh/action`, not to a page-specific URL. The runtime serializes ordinary fields as JSON, applies native constraint validation, marks the form busy, and rerenders the owning page/component from the action response. File inputs are deliberately excluded: use a dedicated authenticated upload endpoint for binaries.

```html
<form data-gosh-form="save-profile">
 <label>Email <input type="email" name="email" required /></label>
 <label>Name <input name="name" required /></label>
 <button type="submit">Save</button>
</form>
```

In Go, submitted fields are isolated from SSR props under `props.Form()`. Treat all of them as untrusted input and validate/authorize on the server.

```go
func (Profile) Action(ctx *logic.Context, event string, props logic.Props) (logic.Data, bool, error) {
    if event != "save-profile" { return nil, false, nil }
    form := props.Form()
    email, _ := form["email"].(string)
    // Validate email, check the current session and persist the profile.
    return logic.Data{"saved": true, "email": email}, true, nil
}
```

For client-side validation and reactive state, call `_gosh.useForm(form, { event, validate, resetOnSuccess })`. It returns `{ pending, errors, values, submit(), reset(), subscribe() }` and removes any existing HTML `action` from the bound form. `validate(fields, form)` may return `false` or an object of field errors. The endpoint requires `X-Runtime: 1`, `X-GOSH-Runtime: action`, and rejects a mismatched browser `Origin`; these checks reduce browser CSRF exposure, but are not a replacement for rate limits, session/JWT authorization, CAPTCHA/challenge policies, or server-side validation against automated abuse.

Place Tailwind directives such as `@theme`, `@utility`, `@variant`, and `@apply` in `web/system/tailwind/global.css`; it is included immediately after `@import "tailwindcss"`.

After Tailwind generates the complete stylesheet, goserver runs the PostCSS pipeline in `web/system/tailwind/postcss.config.mjs` before CSS is hashed, served, or embedded in a production binary. The included plugins add `dvh`/`dvw` declarations to Tailwind screen utilities, add `vh`/`vw` fallbacks for modern viewport units, and remove empty custom properties. To add another final-CSS transformation, create an ES module in `web/system/tailwind/plugins/` and add its plugin instance to that config; no Go, esbuild, or asset-delivery changes are required.

#### CSS layers and local imports

The generated shared stylesheet is assembled in this order:

1. `web/system/tailwind/global.css` — framework Tailwind configuration.
2. `web/system/css/default.css` — package-owned baseline rules.
3. `web/css/default.css` — optional consuming-project overrides.
4. `web/global/styles/*.gosh` — reusable global GOSH styles.

Do not edit the system default file for project-specific design. Create `web/css/default.css` instead; it is appended after the system layer, so normal CSS cascade rules let it override framework defaults. Both default files support recursive local CSS imports. Imported files must stay inside the directory of their entry file and are inlined into the generated stylesheet during development and production builds:

```css
/* web/css/default.css */
@import "./tokens.css";
@import "./layers/forms.css";

:root {
 --brand: #2563eb;
}
```

### Runtime `useQuery`

Client modules can call `_gosh.useQuery({ key, query })` for arbitrary requests or use the `url` shorthand. Queries deduplicate equal keys in a tab, cache results for `staleTime` (30 seconds by default), retry twice with exponential backoff, and can use `broadcast: true` to coordinate the same query across tabs. External URLs are subject to normal browser CORS rules.

SPA page navigation is not cached by default. Set `routeRules` with `cache.maxAge` or `swr` to opt a route into both its server render cache and the bounded browser navigation cache; otherwise goserver emits `Cache-Control: private, no-store` and a repeated navigation fetches fresh output. The browser cache remains LRU-bounded (30 entries by default), and a repeated URL replaces its existing entry rather than accumulating copies.

```js
const users = _gosh.useQuery({
 key: ["users", 15],
 url: "/api/users/15",
 immediate: false,
 staleTime: 60_000,
});
const data = await users.refresh();
```

Use `_gosh.invalidateQuery(key)`, `_gosh.cancelQuery(key)`, and `_gosh.optimisticWrite(key, updater)` for cache control. The runnable `/use-query` page shows local single-flight, an external request, and an optimistic update.

## Templates, static assets, and i18n

### Templates

`TemplateManager` provides layouts, reusable partials, automatic page routes, global and page-specific assets, dynamic variables, and default values on top of Go's `html/template` escaping.

#### Directory structure

```text
templates/
├── layouts/      # base.html, alt.html, and other page layouts
├── pages/        # index.html, contact.html, about@alt.html
├── partials/     # footer.html, header.html, and reusable definitions
├── head/         # global.html and page-specific <head> additions
├── styles/       # global.html and page-specific <style> fragments
└── scripts/      # global.html and page-specific <script> fragments
```

Create the manager once during startup. `TemplatesMiddleware` is optional: use it when every page file should automatically become an HTTP endpoint.

```go
tm := goserver.NewTemplateManager()
s.TemplatesMiddleware(tm)
```

`pages/index.html` maps to `/`, `pages/contact.html` maps to `/contact`, and other page filenames map to the same path without `.html`. A route registered after `TemplatesMiddleware` can replace an automatically generated static page with a dynamic handler.

The default layout name is `base.html`. If `templates/layouts/base.html` is absent, the embedded base layout is used. Select another layout in a page filename with `@`: `pages/about@alt.html` is exposed as `/about` and rendered through `templates/layouts/alt.html`.

#### Page templates

Every page rendered by the bundled base layout defines `content`:

```html
{{define "content"}}
<h1>Contact us</h1>

<p>Phone: {{var . "phone"}}</p>

<ul>
 {{range (var . "emails")}}
 <li>{{.}}</li>
 {{end}}
</ul>

<ul>
 {{range (var . "contacts")}}
 <li>{{index . "name"}} — {{index . "email"}}</li>
 {{end}}
</ul>
{{end}}
```

The `var` helper reads `PageData.Variables` and can also resolve `PageData` fields case-insensitively. The `default` helper returns a fallback for nil, empty, or whitespace-only values:

```html
<p>{{default "Phone is not specified" (var . "phone")}}</p>
```

Variables are also directly accessible through `.Variables`:

```html
{{define "content"}}
<p>Phone: {{index .Variables "phone"}}</p>
<ul>
 {{range index .Variables "emails"}}
 <li>{{.}}</li>
 {{end}}
</ul>
{{end}}
```

#### Custom layouts

An alternate layout must define a template whose name matches its filename without `.html`. For `templates/layouts/alt.html`:

```html
{{define "alt"}}
<!doctype html>
<html lang="en" class="nojs">
 <head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>{{.Title}}</title>
  <meta name="description" content="{{.Description}}" />
  {{.GlobalHead}} {{.PageHead}} {{.GlobalStyles}} {{.PageStyles}}
 </head>
 <body>
  <header><h1>Alternate layout</h1></header>
  <main>{{template "content" .}}</main>
  {{template "footer.html" .}} {{.GlobalScripts}} {{.PageScripts}}
 </body>
</html>
{{end}}
```

#### Partials

Files from `templates/partials` are parsed together with every page and can be included by their defined name. For `templates/partials/footer.html`:

```html
{{define "footer.html"}}
<footer>
 <p>© 2026 MyelophOne</p>
</footer>
{{end}}
```

Include it from a layout or page with:

```html
{{template "footer.html" .}}
```

#### Global and page-specific assets

| Location                         | Available field  | Scope                |
| -------------------------------- | ---------------- | -------------------- |
| `templates/head/global.html`     | `.GlobalHead`    | Every rendered page. |
| `templates/styles/global.html`   | `.GlobalStyles`  | Every rendered page. |
| `templates/scripts/global.html`  | `.GlobalScripts` | Every rendered page. |
| `templates/head/contact.html`    | `.PageHead`      | Only `contact.html`. |
| `templates/styles/contact.html`  | `.PageStyles`    | Only `contact.html`. |
| `templates/scripts/contact.html` | `.PageScripts`   | Only `contact.html`. |

Asset files are inserted as trusted `template.HTML`. They must contain their own `<style>` or `<script>` tags and should only contain application-controlled markup, never untrusted user input.

The embedded base script replaces the `nojs` class on `<html>` with `js`, allowing progressive-enhancement rules for both states.

#### Dynamic rendering

A handler can provide page metadata, arbitrary content, and typed variables:

```go
s.GET("/demo", func(w http.ResponseWriter, r *http.Request) {
	data := goserver.PageData{
		Title:       "Contact",
		Description: "Ways to contact the team",
		GlobalHead:    tm.GlobalHead,
		GlobalStyles:  tm.GlobalStyles,
		GlobalScripts: tm.GlobalScripts,
		Variables: map[string]any{
			"phone":  "+1-234-567-890",
			"emails": []string{"info@example.com", "support@example.com"},
			"contacts": []map[string]string{
				{"name": "Alice", "email": "alice@example.com"},
				{"name": "Bob", "email": "bob@example.com"},
			},
		},
	}

	htmlContent, err := tm.RenderHTML("demo.html", data)
	if err != nil {
		s.RenderError(w, r, http.StatusInternalServerError, "template rendering failed")
		return
	}

	s.RespondHTML(w, htmlContent)
})
```

`RenderHTMLString` renders a standalone template string when the filesystem layout/page system is not needed. The source examples are also available in [`templates/_examples.md`](./templates/_examples.md).

### Static files

Put public files in `./assets` and install `StaticAssetsMiddleware` (included in `Defaults`). `/assets/app.css` resolves to `assets/app.css`; existing public files are also available at root paths such as `/icon.svg`. Development uses the public disk layers, with local files overriding the goserver base. The production distribution serves ordinary public files from its on-disk `assets/` directory; it does not embed images, favicons, downloads or arbitrary public files. The only public asset embedded by the web production build is the resolved `robots.txt`, if present. Public middleware supports GET/HEAD without changing the original request URL.

### Internationalization

Translation files live at `i18n/<language>/<namespace>.json`. Nested JSON keys are flattened, so `i18n/en/account.json` containing `{"profile":{"title":"Profile"}}` becomes `account.profile.title`.

```go
// Register application translations before constructing I18n.
s.RegisterI18nFS(os.DirFS("."))
i18n, err := s.NewI18n("en", []string{"en", "de", "pl", "ru"})
if err != nil {
	log.Fatal(err)
}

s.GET("/hello", func(w http.ResponseWriter, r *http.Request) {
	message := i18n.Lf(r.Context(), "app.hello", map[string]string{"name": "Ada"})
	s.RespondText(w, message)
})
```

Language comes from the first URL segment when supported, then the `hl` query parameter takes precedence. The lookup falls back to the default language and finally returns the key. Use `Lang` / `Langf` for an explicit language, `Pluralf` / `PluralLangf` for plural forms, and `ShortPluralf` plus `FormatShortCount` for compact counts such as `1.2K`.

## Outbound HTTP, proxies, and HTML parsing

### Resilient HTTP client

`HttpClient` adds a shared high-capacity transport, cookie jar, browser-like session headers, timeout, retries with backoff, per-host circuit breakers, optional proxy selection, DNS caching, and SSRF protection that rejects loopback, private, link-local, unspecified, and multicast addresses.

```go
client := goserver.NewHttpClient(
	goserver.WithTimeout(10*time.Second),
	goserver.WithMaxRetries(3),
	goserver.WithCBMaxFailures(5),
	goserver.WithCBResetTimeout(30*time.Second),
	goserver.WithRandomDelayMin(0),
	goserver.WithRandomDelayMax(0),
	goserver.WithEnableMetrics(true),
	goserver.WithMetricsCallback(func(m goserver.Metrics) {
		log.Printf("host=%s status=%d retries=%d duration=%s", m.Host, m.StatusCode, m.RetryCount, m.RequestDuration)
	}),
)

resp, err := client.Get(ctx, "https://example.com", map[string]string{"Accept": "application/json"})
resp, err = client.PostJSON(ctx, "https://api.example.com/items", payload)
status, err := client.CheckStatus(ctx, "https://example.com/file")
```

Other methods are `Do`, `Post`, `Cookies`, and `ResetCookies`. Call `ClearDNSCache`, `ClearCircuitBreakers`, or `GetCircuitBreakerStats` for process-wide client state. `GetBrowserSimpleHeaders` returns a randomized user agent and accept language.

The circuit breaker is shared by hostname (including the port) across client
instances. It counts failed attempts, including retries. To disable it for a
particular client while keeping timeouts and retries:

```go
client := goserver.NewHttpClient(
	goserver.WithDisableCircuitBreaker(true),
	goserver.WithMaxRetries(5),
	goserver.WithTimeout(7*time.Second),
)
```

With the breaker enabled, if it interrupts retries after an actual failed attempt,
`Do` returns that attempt's transport error or HTTP response, with the response
body still open for the caller to read and close. A subsequent request rejected
before any attempt returns `*CircuitBreakerOpenError`, containing the host and
the previous failure (transport error or HTTP status; response bodies are not
retained in shared breaker state). Use
`errors.Is(err, goserver.ErrCircuitBreakerOpen)` instead of direct error equality.
`errors.As` can retrieve `*goserver.CircuitBreakerOpenError` and its `Cause`.

HTTP 401/403 responses do not trigger retries or breaker failures unless
`WithProxyFunc` is configured. HTTP 429 and 5xx responses do. Caller context
cancellation does not count as an upstream failure. Retrying a request with a
body requires `Request.GetBody`; the built-in `Post` and `PostJSON` helpers
provide it. Non-replayable bodies are sent only once.

`WithAllowInsecureSSRF(true)` permits private/internal destinations and should only be used for explicitly trusted URLs. Redirected requests still pass through the same transport checks.

### Optional browser TLS

```go
client := goserver.NewHttpClient(
	goserver.WithBrowserTLS(true),
	goserver.WithTimeout(15*time.Second),
)
defer client.CloseIdleConnections()
```

`WithBrowserTLS(true)` randomly selects a session User-Agent from the existing
`userAgents` list, restricted to Chrome/Firefox versions with a matching profile
in [`tls-client`](https://github.com/bogdanfinn/tls-client). Unsupported entries
(including Safari, iOS and Edge for now) are excluded rather than paired with an
unrelated handshake. The selected User-Agent, TLS ClientHello, HTTP/2 settings and
header ordering stay together for the lifetime of the client. Chrome extension
order is randomized; Firefox does not send Chromium client hints. Explicit request
headers still override session defaults, so overriding User-Agent can introduce a
profile mismatch. HTTP/1.1 fallback, certificate verification,
cookies, redirects, retries and HTTP/HTTPS/SOCKS5 proxies are supported. Plain HTTP
continues to use the standard transport. HTTP/3 is disabled.

The option defaults to `false`; existing clients keep their standard transport.
No build tags are required. Browser transport instances are created on the first
HTTPS request and their connection pools are isolated by proxy (up to eight
retained pools per client). Dependencies are ordinary Go modules resolved during
build/install, not downloaded dynamically when the option is enabled.

This approximates a browser's network fingerprint, not a full browser environment
or a guarantee against bot detection by Instagram or other services. IP reputation,
cookies, JavaScript signals and request behavior still matter. The pinned browser
profiles and UA list need maintenance as browser versions change.
`Response.TLS` may be nil for HTTP/1.1 in this mode due to the underlying transport;
certificate verification remains enabled. As with the standard client, SSRF checks
apply to directly dialed addresses, including the proxy endpoint; a proxy resolves
and reaches the destination itself, so use only trusted proxies.

### Proxy rotation

```go
provider := &goserver.WebshareProvider{Token: os.Getenv("WEBSHARE_TOKEN")}
manager := goserver.NewProxyManager(10*time.Minute, provider)

if err := manager.UpdateSynchronously(ctx); err != nil {
	log.Print(err)
}
manager.StartAutoUpdate(ctx, 30*time.Minute)

client := goserver.NewHttpClient(
	goserver.WithProxyFunc(manager.GetRandomProxy),
	goserver.WithOnProxyError(manager.MarkBad),
)
```

Implement `ProxyProvider.FetchProxies` to add other providers. `WithProxyURL` configures a fixed proxy.

### HTML DOM helpers and soft-404 detection

```go
doc, err := goserver.ParseHTML(`<main><h1 id="title">Hello</h1></main>`)
title := doc.QuerySelector("main #title")
title.SetAttribute("class", "headline")
_ = title.InsertHTML("afterend", `<p class="lead">Welcome</p>`)
html := doc.OuterHTML()
```

Supported selectors are tag, `#id`, `.class`, combinations, and descendant chains. DOM helpers include attribute access, text/inner/outer HTML, append/remove, before/after insertion, and element/text-node constructors.

`CheckSoft404` examines a response status and up to 256 KiB of its title:

```go
err := goserver.CheckSoft404(resp, goserver.Soft404Rules{
	Exact:    []string{"404", "Not Found"},
	Contains: []string{"page not found", "does not exist"},
})
if errors.Is(err, goserver.ErrSoft404) {
	log.Printf("remote page is missing: %s", resp.Request.URL)
}
```

The function consumes the response body while tokenizing it; buffer or replace the body first if it must be read again.

## WebSockets

`WebSocketHub` provides named in-process channels, bounded client send queues, ping/pong handling, a 512-byte inbound message limit, broadcasting, and an optional `WSBroker` for multi-process fan-out.

The wire protocol is intentionally small: after connecting, the client's first text message is the channel name; later messages are broadcast to that channel.

```go
hub := goserver.NewWebSocketHub()
secret := os.Getenv("WS_TOKEN_KEY")

http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
	if err := goserver.ValidateWSToken(r.URL.Query().Get("token"), secret); err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	hub.HandleWebSocket(w, r)
})

log.Fatal(http.ListenAndServe(":8081", nil))
```

Server-side publishing is `hub.Broadcast("orders", payload)`. For horizontal scaling, implement `WSBroker`, then call `hub.SetBroker(broker, "goserver:websocket")`.

For the normal goserver server, attach the hub before `Run`. This exposes the same-origin endpoint at `/ws`; `SetWebSocketAuthorizer` runs immediately before the upgrade. Use it for every private or tenant-specific channel because the hub itself treats channel names as transport identifiers, not permissions.

```go
hub := goserver.NewWebSocketHub()
s.SetWebSocketHub(hub)
s.SetWebSocketAuthorizer(func(r *http.Request) error {
    // Validate the goserver session, JWT, signed WS token, or tenant here.
    return nil
})
```

### GOSH WebSocket composable

The web framework exposes `_gosh.useWebSocket(options)`. It is intentionally an optional chunk: the core runtime never downloads WebSocket code unless a client module calls the composable. The chunk waits for `domReady`, then connects to `/ws` by default. That is the right default for page modules and islands: DOM is available before callbacks run, while connection setup remains independent of rendering. For a connection needed immediately after runtime boot, call the composable in a client module as soon as it mounts.

The composable returns a `Promise` because its chunk is dynamic. It resolves to a reusable connection per `{url, channel}` with `status`, `connected`, `send(value)`, `reconnect()`, `close()`, `release()`, and `subscribe(listener)`. `release()` closes and forgets the shared connection; use it from a component’s unmount cleanup only when no other part of the page should keep that channel open.

```html
<script>
 export async function mount({ element }) {
  const orders = await _gosh.useWebSocket({
   channel: "orders",
   json: true,
   onMessage(message) {
    console.info("order update", message);
   },
   onError(error) {
    console.error("orders socket", error);
   },
  });

  const unsubscribe = orders.subscribe((state) => {
   element.dataset.socketState = state.status;
  });
  return () => unsubscribe();
 }
</script>
```

`json: true` applies `JSON.stringify` to sent values and `JSON.parse` to received values. Without it, the hub’s text payload is passed through unchanged. Override `encode` and `decode` for another protocol, or pass `url: "/ws?token=..."` / a full `wss://` URL when appropriate. Reconnection is enabled by default with exponential backoff from 500ms to 10s; configure `reconnect`, `reconnectDelay`, and `maxReconnectDelay` per connection.

The server protocol sends the channel name as its first frame and accepts at most 512 bytes for every client frame. Keep messages compact and authorize the upgrade before allowing a client to choose a sensitive channel.

## Cron, email, Telegram, and multi-tenancy

### Cron and tracked background jobs

```go
s.Cron.AddIntervalJob("refresh", 5*time.Minute, func(ctx context.Context) error {
	return refresh(ctx)
})

if err := s.Cron.AddDailyJob("cleanup", "03:30", cleanup); err != nil {
	log.Fatal(err)
}

s.Cron.Start()
```

Jobs recover and log their own panics/errors. `Server.Start` stops the cron manager during graceful shutdown; start it explicitly after registering jobs.

### Email

Configure `SMTP_HOST`, `SMTP_PORT`, `SMTP_USER`, `SMTP_PASS`, and `SMTP_FROM`, then create a mailer:

```go
mailer := s.NewMailer()
err := mailer.SendEmailAsync(goserver.EmailMessage{
	To:      []string{"user@example.com"},
	Subject: "Welcome",
	Text:    "Welcome to the service.",
	HTML:    "<h1>Welcome</h1>",
	Files:   []string{"./tmp/report.pdf"},
	Inline:  []string{"./tmp/logo.png"},
})
```

`SendEmail` blocks; `SendEmailAsync` uses a bounded queue and returns an error when full. Files listed in `Files` and `Inline` are treated as temporary and removed after processing.

### Telegram

```go
bot := goserver.NewTelegramBot(os.Getenv("TELEGRAM_BOT_TOKEN"))
err := bot.SendMessage(chatID, "<b>Deployment complete</b>")
err = bot.SendPhotoURL(chatID, "https://example.com/chart.png", "Metrics")
err = bot.SendDocument(chatID, "./tmp/report.pdf", "Daily report")
```

The API also supports a local photo, URL-based photo galleries, and `SendMessageWithPhoto`. Text and captions use Telegram HTML parse mode. Multi-photo groups currently support URLs, not multiple local files.

### Multi-tenancy

```go
tenants := goserver.NewTenantStore()
tenants.AddTenant(&goserver.Tenant{
	ID:   "acme",
	Name: "Acme",
	Config: map[string]any{"domain": "*.acme.example.com"},
})

s.Use(tenants.Middleware(&goserver.TenantMiddlewareConfig{
	HeaderKey: "X-Tenant-ID",
	QueryKey:  "tenant",
	UseDomain: true,
}))

s.GET("/account", func(w http.ResponseWriter, r *http.Request) {
	tenant := goserver.GetTenant(r)
	s.RespondJSON(w, r, tenant)
})
```

Resolution tries the configured header, then query key, then exact or wildcard domain. Missing and unknown tenants receive 403.

## Built-in third-party integrations

The repository already contains direct integrations or ready-to-use adapters for the following systems:

| Service or protocol                     | Integration                       | Typical scenario                                                                            |
| --------------------------------------- | --------------------------------- | ------------------------------------------------------------------------------------------- |
| PostgreSQL                              | `db` package built on pgx v5      | Pools, typed queries, transactions, batches, health checks, slow-query logging.             |
| Redis                                   | `redis` package built on go-redis | Distributed cache and shared session storage.                                               |
| [Webshare.io](https://www.webshare.io/) | `WebshareProvider`                | Download and rotate authenticated proxy lists; mark failed proxies temporarily unavailable. |
| Telegram Bot API                        | `TelegramBot`                     | Operational notifications, formatted messages, photos, galleries, and documents.            |
| SMTP                                    | `Mailer`                          | Synchronous or queued email with text/HTML, attachments, inline images, TLS/STARTTLS.       |
| Prometheus                              | `/metricz` text exposition        | Scrape request, error, connection, uptime, memory, and goroutine metrics.                   |
| Go pprof                                | Protected `/debug/pprof/*` routes | CPU, heap, goroutine, trace, and runtime profiling.                                         |
| Gorilla WebSocket                       | `WebSocketHub`                    | Channel-based realtime messaging with optional external `WSBroker`.                         |

The outbound `HttpClient` is vendor-neutral and can call any HTTP API while supplying retries, circuit breakers, cookie persistence, proxy support, browser headers, DNS caching, and SSRF protection. Interfaces such as `ProxyProvider`, `WSBroker`, `CacheStore`, and `SessionStorage` are the intended extension points for additional vendors.

## Health, metrics, profiling, and operations

### Built-in endpoints

`Run()` registers:

| Endpoint             | Behavior                                                                                                                                                                        |
| -------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /healthz`       | Always reports status, environment, version, uptime, and whether detailed metrics were authorized. Adds connection/request/error/runtime memory metrics with `X-Metrics-Token`. |
| `GET /metricz`       | Prometheus text format when `METRICS_ENABLED=true`; requires `X-Metrics-Token` outside development.                                                                             |
| `GET /debug/pprof/*` | Installed by `Defaults()` when metrics are enabled; requires `?token=<METRICS_SECRET>`.                                                                                         |

```bash
curl -A 'Mozilla/5.0' http://localhost:8080/healthz
curl -A 'Mozilla/5.0' -H "X-Metrics-Token: $METRICS_SECRET" http://localhost:8080/metricz
go tool pprof 'http://localhost:8080/debug/pprof/profile?token=YOUR_TOKEN'
```

`StartPprof()` is a separate development-only helper that starts the default Go pprof server on `localhost:6060`.

### Graceful lifecycle

`Start()` listens for `SIGINT`, `SIGTERM`, and `SIGHUP`. Shutdown marks the server as stopping, rejects new work, cancels cron scheduling and drains HTTP requests. It then waits for running cron jobs, `Go`/`RunAsync` tasks and handlers still running after a timeout, before executing dependency shutdown hooks. `RELOAD_SHUTDOWN_TIMEOUT` bounds this entire process, including blocking legacy hooks; expiration returns an error and closes HTTP connections. Go cannot forcibly terminate callbacks that ignore cancellation: dependency cleanup waits for those callbacks rather than closing their resources underneath them. `Run()` exposes `/readyz` for process readiness (not dependency health); it returns 503 while stopping if the listener is still reachable. `SIGHUP` starts a replacement listener and drains the old one without stopping shared cron jobs; platform socket options determine whether same-address handoff is supported.

Timeouts cancel the request context and can return 504 before a handler exits. When load shedding wraps the timeout middleware, its admission slot remains occupied until that handler actually finishes. DB, Redis and outbound HTTP operations must observe the request context to finish promptly. New `Go`/`RunAsync` submissions after shutdown starts are ignored.

### Docker

```bash
cp .env.example .env
docker compose up --build
```

The Dockerfile uses Node only in the Go builder stage, where the CLI installs its cached toolchain and compiles the production distribution. It does not copy a consumer's `web/system` dependencies or preinstalled `node_modules`. The final image is Alpine with CA certificates and a non-root user; it receives `dist/`, including the executable and public `assets/`, but no Go, Node, Tailwind or project web source tree. Compose includes a `/healthz` health check, restart policy, bounded JSON logs, and configurable host binding through `DOCKER_PORT_BINDING`.

## Configuration

Durations accept Go duration strings such as `500ms`, `15s`, or `2m`; a plain integer is interpreted as seconds. Byte sizes accept integers, `KB`, `MB`, `GB`, or expressions such as `1<<20`.

Most variables are read by `NewServer`, so set them before constructing the server. A practical production `.env` can start with:

```dotenv
APP_ENV=prod
HTTP_PORT=8080
API_PREFIX=/api
APP_ERROR_MODE=json

MAX_BODY_SIZE=2MB
CONCURRENCY_LIMIT=500
RATE_LIMIT_RATE=1000
RATE_LIMIT_WINDOW=1m
ENABLE_GZIP=true
MAINTENANCE_MODE=false
MAINTENANCE_CHECK_INTERVAL=5s
MAINTENANCE_BYPASS_TOKEN=replace-with-a-long-random-token

SESSION_KEY=replace-with-a-long-random-secret
JWT_SECRET=replace-with-another-long-random-secret
WS_TOKEN_KEY=replace-with-another-long-random-secret
CSRF_TRUSTED_ORIGINS=https://app.example.com,https://*.example.net

METRICS_ENABLED=true
METRICS_SECRET=replace-with-a-monitoring-token
```

The following table covers the environment variables consumed by the server, example application, database/mail integrations, and Docker Compose:

| Variable                    | Default                       | Purpose                                                                                                                           |
| --------------------------- | ----------------------------- | --------------------------------------------------------------------------------------------------------------------------------- |
| `HTTP_PORT`                 | `8080` in example app         | Listener port used by `cmd/main.go`.                                                                                              |
| `APP_ENV`                   | `dev`                         | `dev` enables development behavior; production builds set `prod`.                                                                 |
| `LOG_LEVEL`                 | `info`                        | Minimum application log level: `debug`, `info`, `warn`, `error`, `fatal`, or `off`.                                               |
| `LOG_CLIENT_IP`             | `off`                         | Client IP logging: `off`, `masked` (IPv4 last octet/IPv6 host part hidden), or `full`. Masking is still personal-data processing. |
| `APP_ERROR_MODE`            | `html`                        | Default error rendering: `html` or `json`.                                                                                        |
| `API_PREFIX`                | empty                         | External prefix stripped before routing.                                                                                          |
| `MAX_URL_LENGTH`            | `2048`                        | Maximum URL string length.                                                                                                        |
| `MAX_HEADERS`               | `100`                         | Maximum distinct request header keys.                                                                                             |
| `MAX_CONNECTIONS`           | `10000`                       | Active connection limit.                                                                                                          |
| `CONCURRENCY_LIMIT`         | `100`                         | Concurrent requests admitted by load shedding.                                                                                    |
| `MAX_BODY_SIZE`             | `1MB`                         | Maximum request body size.                                                                                                        |
| `MAX_HEADER_BYTES`          | `65536`                       | Header limit on the runtime server.                                                                                               |
| `MaxHeaderBytes`            | `1MB`                         | Legacy case-sensitive setting read by the initial internal server object; prefer `MAX_HEADER_BYTES` for the active listener.      |
| `READ_TIMEOUT`              | `15s`                         | Server read timeout and default handler timeout in `Defaults`.                                                                    |
| `WRITE_TIMEOUT`             | `15s`                         | Server write timeout.                                                                                                             |
| `WRITE_BYTE_TIMEOUT`        | `5s`                          | HTTP/2 timeout for writing a single byte.                                                                                         |
| `IDLE_TIMEOUT`              | `90s`                         | Keep-alive idle timeout.                                                                                                          |
| `READ_HEADER_TIMEOUT`       | `500ms`                       | Header/slowloris deadline.                                                                                                        |
| `PING_TIMEOUT`              | `15s`                         | HTTP/2 ping timeout.                                                                                                              |
| `RELOAD_SHUTDOWN_TIMEOUT`   | `30s`                         | Graceful drain deadline.                                                                                                          |
| `ENABLE_SLOWLORIS_CHECK`    | `false`                       | Loaded compatibility flag; header deadlines are applied independently.                                                            |
| `ENABLE_GZIP`               | `true`                        | Gzip is only applied when `GzipMiddleware` or `WithGzip` is used. Defaults() uses GzipMiddleware.                                 |
| `MAINTENANCE_MODE`          | `false`                       | Static override: when `true`, every request returns `503 Service Unavailable` with a temporary, non-cacheable maintenance page. For dynamic file switching, leave it `false`. |
| `MAINTENANCE_CHECK_INTERVAL` | `5s`                         | Interval for checking for a file named `maintenance` beside the running executable. If the file exists, maintenance mode is enabled; removing it disables maintenance mode without restarting. |
| `MAINTENANCE_BYPASS_TOKEN`  | empty                         | Secret token that permits a request through maintenance mode when sent in the `X-Goserver-Maintenance-Token` header. It is redacted from startup logs. |
| `RATE_LIMIT_SIZE`           | `10000`                       | Number of client IP entries retained by an LRU limiter.                                                                           |
| `RATE_LIMIT_RATE`           | `360`                         | Requests allowed per window.                                                                                                      |
| `RATE_LIMIT_WINDOW`         | `1m`                          | Rate-limit window.                                                                                                                |
| `RATE_LIMIT_SKIP_LOCALHOST` | `true`                        | Exempts loopback clients.                                                                                                         |
| `CSRF_TRUSTED_ORIGINS`      | empty                         | Comma-separated exact/wildcard origins; `*` allows all.                                                                           |
| `SESSION_KEY`               | random per process            | AES-GCM session-cookie passphrase; set a stable secret in production.                                                             |
| `JWT_SECRET`                | random per process            | HS256 signing secret; set a stable secret in production.                                                                          |
| `WS_TOKEN_KEY`              | random per process            | WebSocket token signing secret.                                                                                                   |
| `METRICS_ENABLED`           | `false`                       | Enables `/metricz` and protected pprof routes.                                                                                    |
| `METRICS_SECRET`            | random per process            | Token for detailed health, metrics, and pprof.                                                                                    |
| `TZ`                        | `Europe/Warsaw`               | Application timezone value exposed in config.                                                                                     |
| `DATABASE_URL`              | empty                         | Complete pgx PostgreSQL DSN.                                                                                                      |
| `POSTGRES_HOST`             | empty                         | Host used when `DATABASE_URL` is absent; empty skips DB connection.                                                               |
| `POSTGRES_USER`             | `postgres`                    | PostgreSQL user.                                                                                                                  |
| `POSTGRES_PASSWORD`         | empty                         | PostgreSQL password.                                                                                                              |
| `POSTGRES_DB`               | `postgres`                    | PostgreSQL database.                                                                                                              |
| `DB_EXEC_MODE`              | empty                         | pgx `default_query_exec_mode` when building a DSN.                                                                                |
| `DB_MAX_CONNS`              | `min(max(4, 2 × CPU), 32)`    | Pool maximum; tune against the shared PostgreSQL connection budget across all replicas.                                           |
| `DB_MIN_CONNS`              | `25% of maximum`, minimum `1` | Minimum warm connections; never exceeds `DB_MAX_CONNS`.                                                                           |
| `DB_LOG_MODE`               | `sanitized`                   | `off`, `blind`, `full`, or default SQL-without-args logging.                                                                      |
| `SMTP_HOST`                 | empty                         | SMTP host; empty disables sending.                                                                                                |
| `SMTP_PORT`                 | `25`                          | SMTP port; `465` uses implicit TLS, others opportunistic STARTTLS.                                                                |
| `SMTP_USER`                 | empty                         | SMTP username.                                                                                                                    |
| `SMTP_PASS`                 | empty                         | SMTP password.                                                                                                                    |
| `SMTP_FROM`                 | empty                         | Default sender.                                                                                                                   |
| `SMTP_QUEUE_SIZE`           | `20`                          | Async mail queue capacity.                                                                                                        |
| `SMTP_WORKERS`              | `1`                           | Number of async mail queue workers.                                                                                               |
| `DOCKER_PORT_BINDING`       | `8080`                        | Compose host-to-container port binding.                                                                                           |

`Config.String()` redacts WebSocket/session/JWT/metrics keys plus PostgreSQL and SMTP passwords before logging.

## Utility API

The module includes small public helpers for common service code:

- environment parsing: `GetEnv`, `GetEnvBool`, `GetEnvDuration`, `GetEnvInt`, `GetEnvFloat`, `GetEnvBytes`, `GetEnvBytesInt`;
- client/address data: `GetRealIP`, `IsValidIP`, `IsDev`, `IsProd`, `GetClientType`;
- query parsing: `GetQueryParam` and `GetQueryArray` with defaults and allowlists;
- JSON data: `BytesToJSON`, `GetDotPath`, typed `GetValue` and defaulting/conversion `GetValueOr`;
- sanitization and formatting: `SanitizeXSS`, `SanitizeXSSQuery`, `FormatBytes`;
- HTML/JavaScript extraction: `ExtractJSVar`;
- request params/context: `Params`, `WithParams`, `GetServer`, `MustGetServer`, `GetRequestID`;
- template helpers: `DefaultRenderer`, `GetTemplateVar`, `RenderHTMLString`;
- timing/headers: `RandomDelay`, `CacheControlMiddleware`, `SetHeaders`, `MergeHeaders`;
- diagnostics: `NewError` adds a Go call stack to an error message.

Example for loosely typed JSON:

```go
data, err := goserver.BytesToJSON(body)
name, ok := (goserver.GetValue{}).String(data, "user.profile.name")
age := (goserver.GetValueOr{}).Int(data, "user.age", 0)
firstTag, ok := goserver.GetDotPath(data, "user.tags.0")
```

## Known limitations

These points describe the current public API and should be considered before production adoption:

- `WithMaxConcurrentRequests` currently does not change the outbound HTTP client configuration.
- The main `Server` has no public method to attach its internal WebSocket hub; use a standard `net/http` WebSocket endpoint as shown above.
- The WebSocket upgrader itself accepts every origin. Enforce origin policy in the wrapping handler as well as authenticating the connection.
- `DBStatsCollector.GetStats` currently returns newly initialized counters rather than a snapshot of the recorded counters.
- `CookieOptions.Path` and `CookieOptions.Domain` are currently ignored by `SetCookie`; emitted cookies use path `/` and no explicit domain.
- Redis cache `GetOrSetSWR` currently has the same miss/TTL behavior as `GetOrSet`; it does not serve an expired value while refreshing.
- Hook registries and the default session storage are package-global. Multiple independent servers in the same process share them.
- `GetRealIP` trusts forwarding headers without a trusted-proxy allowlist. Strip untrusted forwarding headers at the edge before using it for rate limits or audit identity.
- `RedirectMiddleware` builds canonical redirect URLs from the request `Host`. Validate the host at a trusted reverse proxy to prevent host-header/open-redirect and cache-poisoning issues.
- `Defaults()` installs the rate limiter once, and `Run()` installs another configured rate limiter when enabled. This is functional but usually redundant.
- The bundled root-package tests include long-running load/external-service scenarios; use targeted tests for fast local feedback.

## Development

The repository uses [Task](https://taskfile.dev/) and [`taskfile.yml`](./taskfile.yml) as its command runner. Install Task before using the commands below.

```bash
# Build from source with Go (cross-platform)
go install github.com/go-task/task/v3/cmd/task@latest

# Or Windows
winget install Task.Task

# Or macOS/Linux with Homebrew
brew install go-task/tap/go-task

task --version
```

The official Task installation page lists additional apt, dnf, apk, npm, Chocolatey, Scoop, and binary options: <https://taskfile.dev/docs/installation>.

Copy `.env.example` to `.env` before running environment-dependent tasks. Task automatically loads `.env` because it is declared in the Taskfile.

### Developer workflows

The workflows below use only tasks already defined by this repository. They document how to combine the existing commands for day-to-day maintenance; no additional Taskfile tasks are required.

Initial checkout and verification:

```bash
git clone https://github.com/myelophone/goserver.git
cd goserver
cp .env.example .env
go mod download
task vet
task test
```

Normal development loop:

```bash
task dev           # live-reload server
task format        # format changed Go files
task vet           # standard Go correctness checks
task test          # complete project test suite
```

Check whether dependencies can be updated without modifying the repository:

```bash
task check-updates
```

Update dependencies, inspect the resulting module diff, then verify it:

```bash
task update
git diff -- go.mod go.sum
task vet
task test
task vulncheck
```

Recommended pre-commit check:

```bash
task format
task lint
task vet
task test
git diff --check
```

Complete static, security, and optimization pass:

```bash
task optimize
task test
task build
```

Before a performance-sensitive release, run the server in production mode in one terminal and load-test it from another:

```bash
task preview
task loadtest
```

For dependency maintenance, `task check-updates` is read-only; `task update` changes `go.mod` and `go.sum`. `task optimize` also changes formatting and may apply betteralign source rewrites, so review its diff before committing.

### All Taskfile commands

| Command                   | What it does                                                                                                           | Additional requirement or note                                                       |
| ------------------------- | ---------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------ |
| `task setup`              | Installs the lockfile-pinned web toolchain into the user cache. | Go and Node.js; network access on first installation. |
| `task setup:client`, `task setup:tailwind` | Compatibility aliases for `task setup`. | No project `web/system` tree required. |
| `task web:generate`       | Generates layered handler/server bindings and the application importer. | Runs setup first; generated files are not committed. |
| `task web:build`          | Builds the production executable and public assets in `dist/`. | Go and Node.js during compilation only. |
| `task web:asset-report`   | Generates bindings and writes `tmp/web-assets.json`. | Runs setup first. |
| `task web:audit`          | Reports public assets without detectable static references. | Does not delete source files. |
| `task web:clean`          | Removes generated build output, bindings/imports and audit reports. | Does not clear the shared toolchain cache. |
| `task run`                | Runs `go run ./cmd` with `APP_ENV=dev`; generates bindings when web is selected. | Go; Node.js when web is enabled. |
| `task dev`                | Runs the server in development mode with live reload and metrics enabled.                                              | Requires `air`; uses `.air.toml`.                                                    |
| `task preview`            | Runs the example server with `APP_ENV=prod`.                                                                           | Go toolchain.                                                                        |
| `task build`              | Builds `dist/goserver` (with the platform executable suffix); selects web or plain-server compilation. | Set `MYELOPHONE_WEB_ENABLED=true` for web; Node.js is needed for web compilation. |
| `task web:build:profile`  | Builds the standalone production web distribution with `Server-Timing` profiling enabled.                              | Use only for profiling; the header exposes server timing details.                    |
| `task profile`            | Selects a web profiling build or a plain-server build. | Web profiling enables `Server-Timing`. |
| `task server -- [args]`   | Runs the executable in `dist/`, forwarding optional arguments. | Run `task build` first; runs from dist; production assets resolve beside the executable. |
| `task lint`               | Runs golangci-lint over the repository.                                                                                | Requires `golangci-lint`.                                                            |
| `task format`             | Formats all Go packages with `go fmt ./...`.                                                                           | Go toolchain.                                                                        |
| `task vet`                | Runs `go vet ./...`.                                                                                                   | Go toolchain.                                                                        |
| `task vulncheck`          | Scans Go call paths for known vulnerabilities.                                                                         | Requires `govulncheck`.                                                              |
| `task test`               | Runs the complete verbose test suite.                                                                                  | Includes long load and external-service scenarios.                                   |
| `task clean`              | Clears the Go module download cache with `go clean -modcache`.                                                         | The next build downloads dependencies again.                                         |
| `task cover`              | Runs tests with coverage, writes `coverage.out` and `coverage.html`, then tries to open the report.                    | Uses `xdg-open` or `open` when available.                                            |
| `task check-updates`      | Shows available updates for all Go modules.                                                                            | Network access.                                                                      |
| `task update`             | Updates dependencies with `go get -u ./...` and runs `go mod tidy`.                                                    | Mutates `go.mod`/`go.sum`; review the diff.                                          |
| `task ba`                 | Reports struct alignment opportunities with betteralign.                                                               | Requires `betteralign`; findings do not fail the task.                               |
| `task bafix`              | Applies betteralign fixes.                                                                                             | Mutates Go sources; requires `betteralign`.                                          |
| `task critic`             | Runs go-critic over all packages.                                                                                      | Requires `go-critic`.                                                                |
| `task osv`                | Recursively scans the source tree with OSV-Scanner.                                                                    | Requires `osv-scanner`.                                                              |
| `task optimize`           | Runs format, lint, vet, govulncheck, OSV-Scanner, go-critic, and betteralign fixes.                                    | Mutates formatting/alignment; requires all analysis tools.                           |
| `task loadtest`           | Runs three Bombardier load stages at 50, 100, and 200 concurrent clients against `http://localhost:8080`.              | Start the server first; requires `bombardier`.                                       |
| `task analyze-web`        | Builds, then opens the Go Size Analyzer web UI for the binary.                                                         | Requires `gsa`.                                                                      |
| `task analyze-tui`        | Builds, then opens the Go Size Analyzer terminal UI.                                                                   | Requires `gsa`.                                                                      |
| `task analyze`            | Builds, then runs the default Go Size Analyzer view.                                                                   | Requires `gsa`.                                                                      |
| `task tag:patch`          | Increments the latest tag's patch component, creates the tag, and pushes it.                                           | Publishing operation; requires Git remote write access and a POSIX-compatible shell. |
| `task tag:minor`          | Increments minor, resets patch, creates the tag, and pushes it.                                                        | Publishing operation; requires Git remote write access and a POSIX-compatible shell. |
| `task tag:major`          | Increments major, resets minor/patch, creates the tag, and pushes it.                                                  | Publishing operation; requires Git remote write access and a POSIX-compatible shell. |
| `task pprof -- <profile>` | Opens a local pprof web UI on `:8080` for a profile file or URL.                                                       | Example: `task pprof -- ./heap.pb.gz`.                                               |

Some Taskfile commands use POSIX shell syntax. On Windows, run those commands from WSL or Git Bash when the active Task shell cannot interpret them.

### Development tools

```bash
go install github.com/air-verse/air@latest
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
go install golang.org/x/vuln/cmd/govulncheck@latest
go install github.com/codesenberg/bombardier@latest
go install github.com/go-critic/go-critic/cmd/go-critic@latest
go install github.com/dkorunic/betteralign/cmd/betteralign@latest
go install github.com/Zxilly/go-size-analyzer/cmd/gsa@v1.12.2
go install github.com/google/osv-scanner/v2/cmd/osv-scanner@latest
go install honnef.co/go/tools/cmd/staticcheck@latest
```

Fast compilation checks that skip the long root load/external scenarios can be run directly when needed:

```bash
go test . -run '^$'   # compile the root package without its long scenarios
go test ./db ./redis  # package tests
go vet ./...          # static correctness checks
```

## GitHub Actions

The repository currently has one workflow: [`Publish Go Module`](./.github/workflows/package.yml). It automates semantic releases for the Go module; it does not build or deploy a server.

### Trigger and permissions

The workflow runs after every push to the `main` branch. Pull requests and tag-only pushes do not trigger it.

It receives these repository permissions:

- `contents: write` — required to create Git tags and GitHub Releases;
- `packages: write` — currently granted, although the workflow does not upload an artifact to GitHub Packages.

The job uses `ubuntu-latest` and the latest available patch release in the Go `1.27` line.

### What the workflow does

| Step                | Behavior                                                                                                                                                                                                                                                                                                         |
| ------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Checkout            | Uses `actions/checkout@v6` with `fetch-depth: 0`, making the complete commit and tag history available for version calculation.                                                                                                                                                                                  |
| Go setup            | Uses `actions/setup-go@v6` with `go-version: "1.27"`. A minor-only selector resolves to the latest available Go 1.27 patch release.                                                                                                                                                                              |
| Git identity        | Configures `github-actions[bot]` as the Git author used by the release process.                                                                                                                                                                                                                                  |
| Semantic release    | Runs [`go-semantic-release/action@v1`](https://github.com/go-semantic-release/action) with the repository `GITHUB_TOKEN`. It examines commits since the previous release, calculates the next semantic version, and creates the corresponding tag and GitHub Release when the commit history requires a release. |
| Module verification | Runs `go mod tidy`, chooses the most recent Git tag, and executes `go list -m -v github.com/myelophone/goserver@$TAG`. The job fails when that tagged module cannot be resolved.                                                                                                                                 |

There is no separate upload step for the Go module. Publishing a public Go module means making a valid semantic-version Git tag available in the repository; `go list` then verifies that consumers can resolve that version.

### Commit messages and versions

The release action derives versions from [Conventional Commits](https://www.conventionalcommits.org/):

```text
fix: correct request timeout       # patch release
feat: add a middleware             # minor release
feat!: change the public API       # breaking release
```

`allow-initial-development-versions: true` means the first development release starts at `v0.1.0`. Before `v1.0.0`, breaking changes are represented as minor-version increments. Commits that do not imply a release can leave the current tag unchanged.

### Important current behavior

- The workflow does **not** run `go test`, `go vet`, golangci-lint, `govulncheck`, or OSV-Scanner before creating a release. Run the documented pre-commit/release tasks locally until a separate CI verification job is added.
- `go mod tidy` runs only inside the Actions checkout. The workflow does not commit a resulting `go.mod` or `go.sum` diff and does not fail merely because `tidy` changed them.
- If no tag exists, the verification step falls back to `v0.0.1`. That fallback is only used for the resolution check; it does not create the tag.
- Go proxy availability can lag behind creation of a new tag, so the final resolution check may fail transiently even after the GitHub Release was created.
- The workflow uses moving action tags (`@v6`, `@v1`) rather than immutable commit SHAs. Pinning third-party actions to reviewed SHAs provides stronger supply-chain reproducibility.

For a manual release, the Taskfile also provides `task tag:patch`, `task tag:minor`, and `task tag:major`. Those commands create and push tags directly; a tag-only push does not start the current workflow.

## Complete copy-paste example

The following `main.go` demonstrates routing, parameters, middleware, JSON parsing and validation, sessions, JWT, caching, idempotency, request IDs, client classification, and the built-in operational endpoints in one runnable program.

```go
package main

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/myelophone/goserver"
)

type Claims struct {
	UserID int `json:"user_id"`
}

type Greeting struct {
	Message   string `json:"message"`
	Generated string `json:"generated"`
}

func main() {
	s := goserver.NewServer(goserver.GetEnv("HTTP_PORT", "8080"))

	// Enables cached handlers/idempotent response replay and browser sessions.
	s.Cache = goserver.NewCache(1_000, "")
	s.SetSessionStorage(goserver.NewInMemorySessionStorage(1_000))

	// Installs the production-oriented middleware stack.
	s.Defaults()
	s.Use(goserver.ResponseHeader("X-Powered-By", "@myelophone/goserver"))

	s.GET("/", func(w http.ResponseWriter, r *http.Request) {
		s.RespondJSON(w, r, map[string]any{
			"message":    "@myelophone/goserver is running",
			"request_id": goserver.GetRequestID(r.Context()),
			"client":     goserver.GetClientType(r),
			"try": []string{
				"GET /hello/Ada",
				"POST /api/v1/echo",
				"POST /session",
				"GET /session",
				"POST /token",
				"GET /cached",
				"GET /healthz",
			},
		})
	})

	s.GET("/hello/:name", func(w http.ResponseWriter, r *http.Request) {
		name := s.GetParams(r).Get("name")
		s.RespondJSON(w, r, map[string]string{"message": "Hello, " + name + "!"})
	})

	api := s.Group("/api/v1")
	api.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				s.RenderErrorJSON(w, r, http.StatusUnsupportedMediaType, "use application/json")
				return
			}
			next.ServeHTTP(w, r)
		})
	})

	api.POST("/echo", func(w http.ResponseWriter, r *http.Request) {
		input, err := goserver.ParseRequest(r)
		if err != nil {
			s.RenderErrorJSON(w, r, http.StatusBadRequest, err.Error())
			return
		}

		errs := input.Validate([]goserver.ValidationRule{
			{Name: "email", Type: "string", Required: true, Regex: goserver.RegexEmail},
			{Name: "age", Type: "int", Required: true, Min: goserver.FloatPtr(18)},
		})
		if len(errs) != 0 {
			s.RenderErrorJSON(w, r, http.StatusUnprocessableEntity, errs[0].Error())
			return
		}

		age, ok := input.GetInt("age")
		if !ok {
			s.RenderErrorJSON(w, r, http.StatusUnprocessableEntity, "age must be an integer")
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusCreated)
		s.RespondJSON(w, r, map[string]any{"email": input.GetString("email"), "age": age})
	})

	s.POST("/session", func(w http.ResponseWriter, r *http.Request) {
		id := s.SessionStart(&w, r)
		id.SetSessionValue("visits", 1)
		s.RespondJSON(w, r, map[string]bool{"session_started": true})
	})

	s.GET("/session", func(w http.ResponseWriter, r *http.Request) {
		id := s.SessionStart(&w, r)
		s.RespondJSON(w, r, id.GetAllSessionValues())
	})

	s.POST("/token", func(w http.ResponseWriter, r *http.Request) {
		token, err := goserver.GenerateToken(s.JWT, Claims{UserID: 42}, 15*time.Minute)
		if err != nil {
			s.RenderErrorJSON(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		s.RespondJSON(w, r, map[string]string{"token": token})
	})

	s.GET("/cached", func(w http.ResponseWriter, r *http.Request) {
		value, err := goserver.Fetch(r.Context(), s.Cache, "demo:greeting", 30*time.Second,
			func(ctx context.Context) (Greeting, error) {
				return Greeting{
					Message:   "generated once, then served from cache",
					Generated: time.Now().Format(time.RFC3339Nano),
				}, nil
			},
		)
		if err != nil {
			s.RenderErrorJSON(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		s.RespondJSON(w, r, value)
	})

	log.Printf("open http://localhost:%s", goserver.GetEnv("HTTP_PORT", "8080"))
	s.Run()
}
```

Run and exercise it:

```bash
go mod init example.com/goserver-demo
go get github.com/myelophone/goserver
go run .

curl -A 'Mozilla/5.0' http://localhost:8080/
curl -A 'Mozilla/5.0' http://localhost:8080/hello/Ada
curl -A 'Mozilla/5.0' -H 'Content-Type: application/json' \
  -d '{"email":"ada@example.com","age":37}' http://localhost:8080/api/v1/echo
curl -A 'Mozilla/5.0' -H 'Idempotency-Key: token-demo-1' \
  -X POST http://localhost:8080/token
curl -A 'Mozilla/5.0' http://localhost:8080/cached
```

To see the same routes under a common external prefix, start with `API_PREFIX=/service`; `/hello/Ada` then becomes `/service/hello/Ada`.

## License

Copyright © 2026 Aliaksandr Ivanou ([aleksivanov.me](https://aleksivanov.me), [GitHub](https://github.com/aleksivanou)). All rights reserved.

This project is licensed under the **PolyForm Noncommercial License 1.0.0**. See [`LICENSE`](./LICENSE) for the full text. Commercial use is not permitted by this license without separate permission from the copyright holder.
