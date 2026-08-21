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

The defaults are intentionally opinionated: request IDs, access logging, load shedding, panic recovery, rate limiting, static assets, redirects, request filtering, URL sanitization, CSRF protection, security headers, timeouts, body limits, client classification, and idempotency are installed together. If those defaults do not fit a service, construct a smaller stack one middleware at a time.

The implementation is performance-conscious and includes pooled buffers, bounded LRU caches, singleflight request coalescing, connection limits, graceful shutdown, and overload protection. As with any Go HTTP stack, actual allocations and throughput depend on the handlers, middleware, encoders, and deployment; benchmark your own workload before making latency or allocation guarantees.

## Contents

- [Requirements and installation](#requirements-and-installation)
- [Why @myelophone/goserver](#why-myelophonegoserver)
- [Quick start](#quick-start)
- [How the server is assembled](#how-the-server-is-assembled)
- [Extending @myelophone/goserver](#extending-myelophonegoserver)
- [Routing and route groups](#routing-and-route-groups)
- [Requests, validation, and responses](#requests-validation-and-responses)
- [Middleware](#middleware)
- [Errors, hooks, and background work](#errors-hooks-and-background-work)
- [Cookies, sessions, JWT, and encryption](#cookies-sessions-jwt-and-encryption)
- [Caching and idempotency](#caching-and-idempotency)
- [PostgreSQL](#postgresql)
- [Redis](#redis)
- [Templates, static assets, and i18n](#templates-static-assets-and-i18n)
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
- PostgreSQL and Redis are optional and only needed for their corresponding packages.
- Docker is optional.

Install the module in an application:

```bash
go get github.com/myelophone/goserver
```

The repository itself includes a runnable example in [`cmd/main.go`](./cmd/main.go):

```bash
cp .env.example .env
go run ./cmd/main.go
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
15. idempotency.

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

`Fetch[T]` uses cache-aside semantics and singleflight to collapse concurrent misses. `FetchSWR[T]` returns stale data after expiry and triggers a background refresh. Both support structs, strings, and byte slices.

Direct `CacheStore` operations are also available:

```go
_ = cache.Set(ctx, "key", value, time.Minute)
value, found := cache.Get(ctx, "key")
bytes, err := cache.GetOrSet(ctx, "key", time.Minute, generator)
```

### Idempotent write endpoints

`Defaults()` installs `IdempotencyMiddleware`. Assign `s.Cache` to persist successful replayable responses for 24 hours; without a cache, concurrent duplicate requests are still coalesced during the in-flight call.

```bash
curl -A 'Mozilla/5.0' \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: order-2026-0001' \
  -d '{"sku":"ABC","quantity":1}' \
  http://localhost:8080/orders
```

The cache key combines method, URL path, and `Idempotency-Key`. Replayed concurrent responses include `Idempotent-Replayed: true`.

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

Put public files in `./assets` and install `StaticAssetsMiddleware` (included in `Defaults`). `/assets/app.css` resolves to `assets/app.css`. `PublicFiles` also serves `/robots.txt` from the same directory.

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

`WithAllowInsecureSSRF(true)` permits private/internal destinations and should only be used for explicitly trusted URLs. Redirected requests still pass through the same transport checks.

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

At present the main `Server` does not expose a public setter for its internal WebSocket hub. Mount the hub on a standard `net/http` route/server as above; middleware wrapped around the upgrade must preserve `http.Hijacker`.

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

`Start()` listens for `SIGINT`, `SIGTERM`, and `SIGHUP`. Shutdown runs registered hooks, drains the HTTP server up to `RELOAD_SHUTDOWN_TIMEOUT`, stops cron jobs, and waits for `RunAsync` tasks. `SIGHUP` starts a replacement listener and gracefully shuts down the previous server; platform socket options determine whether the same-address handoff is supported.

### Docker

```bash
cp .env.example .env
docker compose up --build
```

The image uses a static, non-root distroless runtime. Compose includes a `/healthz` health check, restart policy, bounded JSON logs, and configurable host binding through `DOCKER_PORT_BINDING`.

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

SESSION_KEY=replace-with-a-long-random-secret
JWT_SECRET=replace-with-another-long-random-secret
WS_TOKEN_KEY=replace-with-another-long-random-secret
CSRF_TRUSTED_ORIGINS=https://app.example.com,https://*.example.net

METRICS_ENABLED=true
METRICS_SECRET=replace-with-a-monitoring-token
```

The following table covers the environment variables consumed by the server, example application, database/mail integrations, and Docker Compose:

| Variable                    | Default               | Purpose                                                                                                                      |
| --------------------------- | --------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| `HTTP_PORT`                 | `8080` in example app | Listener port used by `cmd/main.go`.                                                                                         |
| `APP_ENV`                   | `dev`                 | `dev` enables development behavior; production builds set `prod`.                                                            |
| `APP_ERROR_MODE`            | `html`                | Default error rendering: `html` or `json`.                                                                                   |
| `API_PREFIX`                | empty                 | External prefix stripped before routing.                                                                                     |
| `MAX_URL_LENGTH`            | `2048`                | Maximum URL string length.                                                                                                   |
| `MAX_HEADERS`               | `100`                 | Maximum distinct request header keys.                                                                                        |
| `MAX_CONNECTIONS`           | `10000`               | Active connection limit.                                                                                                     |
| `CONCURRENCY_LIMIT`         | `100`                 | Concurrent requests admitted by load shedding.                                                                               |
| `MAX_BODY_SIZE`             | `1MB`                 | Maximum request body size.                                                                                                   |
| `MAX_HEADER_BYTES`          | `65536`               | Header limit on the runtime server.                                                                                          |
| `MaxHeaderBytes`            | `1MB`                 | Legacy case-sensitive setting read by the initial internal server object; prefer `MAX_HEADER_BYTES` for the active listener. |
| `READ_TIMEOUT`              | `15s`                 | Server read timeout and default handler timeout in `Defaults`.                                                               |
| `WRITE_TIMEOUT`             | `15s`                 | Server write timeout.                                                                                                        |
| `IDLE_TIMEOUT`              | `90s`                 | Keep-alive idle timeout.                                                                                                     |
| `READ_HEADER_TIMEOUT`       | `500ms`               | Header/slowloris deadline.                                                                                                   |
| `PING_TIMEOUT`              | `15s`                 | HTTP/2 ping timeout.                                                                                                         |
| `RELOAD_SHUTDOWN_TIMEOUT`   | `30s`                 | Graceful drain deadline.                                                                                                     |
| `ENABLE_SLOWLORIS_CHECK`    | `false`               | Loaded compatibility flag; header deadlines are applied independently.                                                       |
| `ENABLE_GZIP`               | `true`                | Adds gzip middleware in `Run`.                                                                                               |
| `RATE_LIMIT_SIZE`           | `10000`               | Number of client IP entries retained by an LRU limiter.                                                                      |
| `RATE_LIMIT_RATE`           | `360`                 | Requests allowed per window.                                                                                                 |
| `RATE_LIMIT_WINDOW`         | `1m`                  | Rate-limit window.                                                                                                           |
| `RATE_LIMIT_SKIP_LOCALHOST` | `true`                | Exempts loopback clients.                                                                                                    |
| `CSRF_TRUSTED_ORIGINS`      | empty                 | Comma-separated exact/wildcard origins; `*` allows all.                                                                      |
| `SESSION_KEY`               | random per process    | AES-GCM session-cookie passphrase; set a stable secret in production.                                                        |
| `JWT_SECRET`                | random per process    | HS256 signing secret; set a stable secret in production.                                                                     |
| `WS_TOKEN_KEY`              | random per process    | WebSocket token signing secret.                                                                                              |
| `METRICS_ENABLED`           | `false`               | Enables `/metricz` and protected pprof routes.                                                                               |
| `METRICS_SECRET`            | random per process    | Token for detailed health, metrics, and pprof.                                                                               |
| `TZ`                        | `Europe/Warsaw`       | Application timezone value exposed in config.                                                                                |
| `DATABASE_URL`              | empty                 | Complete pgx PostgreSQL DSN.                                                                                                 |
| `POSTGRES_HOST`             | empty                 | Host used when `DATABASE_URL` is absent; empty skips DB connection.                                                          |
| `POSTGRES_USER`             | `postgres`            | PostgreSQL user.                                                                                                             |
| `POSTGRES_PASSWORD`         | empty                 | PostgreSQL password.                                                                                                         |
| `POSTGRES_DB`               | `postgres`            | PostgreSQL database.                                                                                                         |
| `DB_EXEC_MODE`              | empty                 | pgx `default_query_exec_mode` when building a DSN.                                                                           |
| `DB_MAX_CONNS`              | `8 × CPU`             | Pool maximum; minimum is 50% of maximum.                                                                                     |
| `DB_LOG_MODE`               | `sanitized`           | `off`, `blind`, `full`, or default SQL-without-args logging.                                                                 |
| `SMTP_HOST`                 | empty                 | SMTP host; empty disables sending.                                                                                           |
| `SMTP_PORT`                 | `25`                  | SMTP port; `465` uses implicit TLS, others opportunistic STARTTLS.                                                           |
| `SMTP_USER`                 | empty                 | SMTP username.                                                                                                               |
| `SMTP_PASS`                 | empty                 | SMTP password.                                                                                                               |
| `SMTP_FROM`                 | empty                 | Default sender.                                                                                                              |
| `SMTP_QUEUE_SIZE`           | `20`                  | Async mail queue capacity.                                                                                                   |
| `SMTP_WORKERS`              | `1`                   | Number of async mail queue workers.                                                                                          |
| `DOCKER_PORT_BINDING`       | `8080`                | Compose host-to-container port binding.                                                                                      |

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
| `task run`                | Runs `go run ./cmd/main.go` with `APP_ENV=dev`.                                                                        | Go toolchain.                                                                        |
| `task dev`                | Runs the server in development mode with live reload and metrics enabled.                                              | Requires `air`; uses `.air.toml`.                                                    |
| `task preview`            | Runs the example server with `APP_ENV=prod`.                                                                           | Go toolchain.                                                                        |
| `task build`              | Builds `./tmp/goserver` with `CGO_ENABLED=0`, trimpath, PGO, stripped symbols, prod environment, and Git hash version. | Go and Git.                                                                          |
| `task view -- [args]`     | Runs the binary produced by `task build`, forwarding optional arguments.                                               | Run `task build` first.                                                              |
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
