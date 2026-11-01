# Junkit

📖 **Documentation: [jundoc.pages.dev](https://jundoc.pages.dev/junkit/)** — visual, bilingual (EN / VI) guides for every package.

A reusable library of infrastructure packages for Go web APIs built on
[Gin](https://github.com/gin-gonic/gin) and [Uber Fx](https://github.com/uber-go/fx). It is the
foundation [`jungo`](https://github.com/jun-develop/jungo) is built on, but every package stands on
its own: take only the ones you need, wire them through Fx with one line, or call their
constructors directly without Fx at all.

```bash
go get github.com/jun-develop/junkit
```

**New here?** Read sections 1 → 3 to understand how the packages fit together and get a minimal app
running (about 10 minutes). Part II is a per-package reference — features, options and usage.
Part III collects recipes that combine several packages.

## Table of contents

**Part I — Getting started**

1. [Requirements](#1-requirements)
2. [Design principles](#2-design-principles)
3. [Quick start: a minimal app](#3-quick-start-a-minimal-app)

**Part II — Package reference**

4. [Package overview](#4-package-overview)
5. [`console`](#5-console) — colored CLI output
6. [`logger`](#6-logger) — zap logger, file rotation, trace IDs
7. [`i18n`](#7-i18n) — message translation
8. [`response`](#8-response) — JSON envelope, `AppError`, field filtering
9. [`validation`](#9-validation) — localized validation errors, custom rules
10. [`pagination`](#10-pagination) — list query params → offset/limit
11. [`database`](#11-database) — driver-independent error classification; `database/postgres` pool, transactions
12. [`cache`](#12-cache) — generic cache: Redis / memory / no-op
13. [`storage`](#13-storage) — file upload/delete
14. [`httpclient`](#14-httpclient) — tuned `*http.Client` with retry
15. [`telegram`](#15-telegram) — Telegram Bot API client
16. [`notification`](#16-notification) — ops alerts to Telegram
17. [`recaptcha`](#17-recaptcha) — reCAPTCHA v3 verification
18. [`security`](#18-security) — tokens, passwords, identity context
19. [`middleware`](#19-middleware) — Gin middleware
20. [`tracer`](#20-tracer) — request debug dashboard
21. [`sqlfmt`](#21-sqlfmt) — SQL parsing/formatting helpers
22. [`scaffold`](#22-scaffold) — code-generation engine

**Part III — Recipes**

23. [Full Fx wiring](#23-full-fx-wiring)
24. [Recommended middleware chain](#24-recommended-middleware-chain)
25. [Repository + transaction + error mapping](#25-repository--transaction--error-mapping)
26. [Bearer-token authentication](#26-bearer-token-authentication)
27. [Debugging a request with `tracer`](#27-debugging-a-request-with-tracer)

**Part IV — Contributing**

28. [Development](#28-development)

---

# Part I — Getting started

## 1. Requirements

| Requirement | Why |
|---|---|
| **Go 1.26.5+** | The module's `go` directive |
| **PostgreSQL** | Only if you use `database/postgres` (driver: [pgx v5](https://github.com/jackc/pgx)) |
| **Redis** | Only if you use `cache` with `DriverRedis` |
| **Telegram bot token + chat ID** | Only if you use `telegram` / `notification` |
| **reCAPTCHA v3 secret key** | Only if you use `recaptcha` without `Mock` |

Main dependencies: `gin`, `fx`, `zap`, `pgx/v5`, `go-redis/v9`, `go-playground/validator/v10`,
`golang.org/x/crypto` (bcrypt), `golang.org/x/time` (rate limiting), `lumberjack` (log rotation).

---

## 2. Design principles

Knowing these four conventions makes every package predictable.

### Every package is configured by a plain `Options` struct

No package reads environment variables or config files. Your app loads config however it likes and
hands each package its `Options`:

```go
fx.Supply(logger.Options{Environment: "production", Level: "info", Filename: "logs/app.log"})
```

### Every package (almost) exposes an Fx `Module`

Registering a package is one line: supply its `Options`, add its `Module`.

```go
fx.New(
	fx.Supply(postgres.Options{DSN: dsn}),
	postgres.Module, // provides *postgres.DB
)
```

Exceptions:

| Package | Why it has no `Module` |
|---|---|
| `console` | Package-level print functions — nothing to inject |
| `pagination` | Pure functions and structs |
| `sqlfmt` | Pure helpers shared by `database/postgres` and `tracer` |
| `middleware` | Call sites need *differently configured* instances (e.g. a stricter `Limiter` for one route group), so you call the constructors where routes are registered |
| `tracer` | Context-based — activated per request by `middleware.TracerDebug` |
| `scaffold` | A build-time code generator, not a runtime dependency |

Every constructor is also exported (`logger.New`, `postgres.NewDatabase`, `cache.New[T]`, ...), so
Fx is never mandatory.

### Depend on interfaces, swap implementations by config

Consumers depend on interfaces — `response.Responder`, `cache.Cache[T]`, `storage.Service`,
`telegram.Client`, `notification.Notifier`, `recaptcha.Verifier`, `security.TokenSealer`,
`security.PasswordHasher` — so the backing implementation (Redis vs. memory cache, real vs. mock
reCAPTCHA, ...) is a config decision, and tests can pass fakes.

### Core packages never import optional ones

`database/postgres` must not import `tracer`, yet the debug dashboard must see every SQL query. The
bridge is an extension point — `postgres.Options.Decorate`:

```go
postgres.Options{
	DSN: dsn,
	Decorate: func(inner postgres.DBTX) postgres.DBTX {
		return tracer.NewPgxWrapper(inner) // zero-overhead unless the request is being debugged
	},
}
```

The same idea keeps `middleware.Recover`/`Limiter` usable when `notification` isn't wired (pass
`nil` or an `optional:"true"` Fx dependency).

---

## 3. Quick start: a minimal app

A runnable Gin + Fx app using the core packages (`logger`, `i18n`, `response`, `validation`,
`middleware`):

```go
package main

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/fx"
	"go.uber.org/zap"

	"github.com/jun-develop/junkit/i18n"
	"github.com/jun-develop/junkit/logger"
	"github.com/jun-develop/junkit/middleware"
	"github.com/jun-develop/junkit/response"
	"github.com/jun-develop/junkit/validation"
)

type createUserRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,password_strong"`
}

func main() {
	fx.New(
		fx.Supply(
			logger.Options{Environment: "development", Level: "debug"},
			response.Options{Language: i18n.LangEN},
			validation.Options{Language: i18n.LangEN},
		),
		logger.Module,
		i18n.Module,
		response.Module,
		validation.Module,
		fx.Invoke(run),
	).Run()
}

func run(lc fx.Lifecycle, log *zap.Logger, responder response.Responder, v *validation.Validator) {
	r := gin.New()
	r.Use(
		middleware.Security(),
		middleware.Trace(middleware.TraceOptions{Version: "v0.1.0", Environment: "development"}),
		middleware.Recover(log, nil, responder, middleware.RecoverOptions{}),
	)

	r.POST("/users", func(c *gin.Context) {
		var req createUserRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			responder.SendWithData(c, http.StatusUnprocessableEntity, "validation_error",
				v.GetValidationErrors(c, err))
			return
		}
		responder.SendWithData(c, http.StatusCreated, "operation_successful", gin.H{"email": req.Email})
	})

	srv := &http.Server{Addr: ":8080", Handler: r}
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error { go srv.ListenAndServe(); return nil },
		OnStop:  srv.Shutdown,
	})
}
```

```bash
curl -s -XPOST localhost:8080/users -d '{"email":"bad","password":"x"}' | jq
```

```json
{
  "status": "error",
  "message": "Validation error",
  "error": {
    "email": "email must be a valid email format",
    "password": "password must be at least 8 characters including (lowercase, uppercase, number, and special character)"
  },
  "api_info": { "trace_id": "…", "timestamp": "…", "latency": "…", "version": "v0.1.0", "env": "development" }
}
```

> The exact field-name formatting and envelope fields are produced by `validation` and `response` —
> see sections [8](#8-response) and [9](#9-validation).

---

# Part II — Package reference

Every exported symbol has a GoDoc comment with a `Usage:` example. This part is the guided tour;
run `go doc -all github.com/jun-develop/junkit/<package>` for the complete API.

## 4. Package overview

| Package | Purpose | Fx | Provides |
|---|---|---|---|
| [`console`](#5-console) | Colored status output for CLI tools and bootstrap code | — | functions |
| [`logger`](#6-logger) | zap logger: console in dev, rotating JSON files elsewhere; trace-ID propagation | `Module` | `*zap.Logger` |
| [`i18n`](#7-i18n) | Thread-safe translator with per-language catalogs and fallback | `Module` | `*i18n.Translator` |
| [`response`](#8-response) | Standard JSON envelope, `AppError` → HTTP status, `?fields=`/`?omit=` filtering | `Module` | `response.Responder` |
| [`validation`](#9-validation) | `go-playground/validator` + custom rules + localized, field-keyed errors | `Module` | `*validation.Validator` |
| [`pagination`](#10-pagination) | Parses page/page_size/search/sort and computes offset/limit + metadata | — | types |
| [`database`](#11-database) | Driver-independent error classification; `database/postgres`: pgxpool lifecycle, context-scoped transactions, query logging | `postgres.Module` | `*postgres.DB` |
| [`cache`](#12-cache) | Generic `Cache[T]` with Redis / in-memory / no-op backends | `Module` | `cache.Cache[any]` |
| [`storage`](#13-storage) | File upload/delete behind an interface (local filesystem shipped); `upload` checks a file before it is stored | `Module` | `storage.Service` |
| [`httpclient`](#14-httpclient) | Production-tuned `*http.Client` with retry + exponential backoff | `Module` | `*http.Client` |
| [`telegram`](#15-telegram) | Minimal Telegram Bot API client (messages, streamed documents) | `Module` | `telegram.Client` |
| [`notification`](#16-notification) | Panic / error / rate-limit alerts to Telegram with a repro `curl` | `Module` | `notification.Notifier` |
| [`recaptcha`](#17-recaptcha) | Google reCAPTCHA v3 verification, with a mock for dev/tests | `Module` | `recaptcha.Verifier` |
| [`security`](#18-security) | Sealed tokens (AES-GCM + HMAC), token hashing, bcrypt, identity in context | `Module` | `TokenSealer`, `PasswordHasher` |
| [`middleware`](#19-middleware) | CORS, rate limit, recover, security headers, trace, payload, access log, route patterns, debug | — | `gin.HandlerFunc`s |
| [`tracer`](#20-tracer) | Request-scoped debug recorder powering the `?<key>=<value>` dashboard | — | functions |
| [`sqlfmt`](#21-sqlfmt) | Parse sqlc annotations, bind args into SQL, locate the calling code | — | functions |
| [`scaffold`](#22-scaffold) | Templated file generation + Fx module (un)registration | — | functions |
| `slicex` | Slice helpers the standard `slices` package lacks: `Map(s, fn)` (empty input → `[]`, so JSON lists never render as `null`) | — | functions |

**Dependency graph** (what each `Module` needs injected):

```
logger.Module        ← logger.Options
i18n.Module          ← (nothing)
response.Module      ← response.Options, *i18n.Translator
validation.Module    ← validation.Options, *i18n.Translator
postgres.Module      ← postgres.Options, *zap.Logger, fx.Lifecycle
cache.Module         ← cache.Options
storage.Module       ← storage.Options
httpclient.Module    ← httpclient.Options
telegram.Module      ← telegram.Options, *http.Client, *zap.Logger
notification.Module  ← notification.Options, telegram.Client
recaptcha.Module     ← recaptcha.Options, *http.Client
security.Module      ← security.Options
```

---

## 5. `console`

Colored, human-oriented status lines for **CLI tools, migration runners, code generators and
`main` bootstrap** — not for request-time application logging (use `logger` for that).

| Function | Output |
|---|---|
| `Successf(format, args...)` | green |
| `Infof(format, args...)` | cyan |
| `Warnf(format, args...)` | yellow, prefixed `Warning:` |
| `Fatalf(format, args...)` | red, prefixed `Error:`, then `os.Exit(1)` |
| `Stepf(icon, format, args...)` | bold, prefixed with `icon` |
| `NewError(format, args...)` | returns a formatted `error` without printing (supports `%w`) |

```go
console.Stepf("🏗️", "generating handler for %s", name)
console.Infof("applying migration %s", file)
console.Warnf("no .env file found, using defaults")
console.Successf("migration applied")
console.Fatalf("cannot connect to database: %v", err) // exits
```

---

## 6. `logger`

Builds a `*zap.Logger`:

- **`Environment: "development"`** → human-readable console output on stdout.
- **any other environment** → JSON lines written to `Filename`, rotated by
  [lumberjack](https://github.com/natefinch/lumberjack) (`MaxSize` MB, `MaxBackups`, `MaxAge` days,
  optional gzip `Compress`). Writes are buffered (256 KB, flushed every second, on Fx stop and on a
  Panic/Fatal entry): one unbuffered write per line took ~17% of a busy API's CPU, behind the file's
  lock. A crash can lose the last second of lines.
- Invalid `Level` values fall back to `info`.
- The logger is flushed (`Sync`) on Fx stop.

**Options**

| Field | Meaning |
|---|---|
| `Environment` | `"development"` → console; otherwise → rotating JSON file |
| `Output` | `logger.OutputConsole` / `logger.OutputFile` overrides the choice made by `Environment` |
| `Level` | `debug`, `info`, `warn`, `error` |
| `Filename` | Log file path (non-development) |
| `MaxSize` / `MaxBackups` / `MaxAge` / `Compress` | Rotation settings |

**Single logger**

```go
fx.Supply(logger.Options{Environment: "production", Level: "info", Filename: "logs/app.log", MaxSize: 10}),
logger.Module, // provides *zap.Logger
```

**Several named channels** — e.g. a separate access log. Each `Definition` gets its own level and
file; the `"app"` channel is also provided untagged as the default `*zap.Logger`:

```go
fx.Options(logger.ProvideNamed([]logger.Definition{
	{Name: "app", Level: "info", Filename: "logs/app.log"},
	{Name: "http", Level: "info", Filename: "logs/http.log"},
}, logger.Options{Environment: "production", MaxSize: 10})...)

// consume the tagged one:
fx.Invoke(fx.Annotate(func(httpLog *zap.Logger) { /* ... */ }, fx.ParamTags(`name:"http"`)))
```

**Trace IDs** — `middleware.Trace` puts the request's trace ID into the context; read it anywhere
below:

```go
ctx = logger.WithTraceID(ctx, traceID)
log.Info("creating user", zap.String("trace_id", logger.GetTraceID(ctx)))
```

---

## 7. `i18n`

A thread-safe `Translator` holding one message catalog per language.

- Ships with a built-in **English** catalog covering every key the other junkit packages emit
  (validation rules, `internal_server_error`, `unauthorized_token`, `too_many_requests`,
  `request_body_too_large`, ...).
- Languages: `i18n.LangEN` (`"en"`, built-in) and `i18n.LangVI` (`"vi"`, bring your own catalog).
- **Fallback chain:** requested language → default language → the key itself (so a missing
  translation never panics or returns empty).
- Messages are `fmt` formats; arguments are applied only if the message contains a verb.

```go
func registerTranslations(t *i18n.Translator) {
	t.AddTranslations(map[string]map[string]string{
		i18n.LangEN: {"user_not_found": "User not found", "hello": "Hello, %s!"},
		i18n.LangVI: {"user_not_found": "Không tìm thấy người dùng", "hello": "Xin chào, %s!"},
	})
}

// fx.Invoke(registerTranslations) — typically once per feature module

msg := t.GetMessage("hello", i18n.LangVI, "Tuấn") // "Xin chào, Tuấn!"
t.SetDefaultLang(i18n.LangVI)                      // "" resets to English
```

`AddTranslations` merges and overwrites existing keys, so you can also override the built-in
English messages.

---

## 8. `response`

The **only** way handlers should write HTTP responses. Gives every endpoint the same envelope and
maps domain errors to HTTP status codes.

### The envelope

```json
{
  "status": "success",
  "code": "NOT_FOUND",
  "message": "User not found",
  "data": { },
  "error": { },
  "pagination": { },
  "api_info": {
    "trace_id": "eb9b3d81-869d-4b62-80b8-5780f2da2f30",
    "timestamp": "2026-01-10T12:44:05Z",
    "latency": "188.280ms",
    "version": "v1.0.0",
    "env": "production"
  }
}
```

Empty fields are omitted. `api_info` is filled from the values `middleware.Trace` stores on the gin
context (`StartTimeContextKey`, `TraceIDContextKey`, `VersionContextKey`, `EnvContextKey`).

### The `Responder` interface

| Method | Use |
|---|---|
| `Send(c, status, key, args...)` | Message-only response; `key` is an i18n key |
| `SendWithData(c, status, key, data, args...)` | `data` goes into `"data"` when `status < 400`, into `"error"` otherwise |
| `Pagination(c, status, key, items, meta, args...)` | Wraps items + `pagination.Pagination` |
| `Error(c, err)` | `*AppError` → its code's HTTP status + translated message; any other error → recorded on the gin context and answered with a generic 500 |
| `Translate(key, args...)` | Resolve a key without responding |

### Domain errors: `AppError`

Define errors once per feature with a code and an i18n key; return them from services; let
`responder.Error` do the rest.

```go
var ErrUserNotFound = response.New(response.NotFound, "user_not_found")

// wrap an underlying cause (kept for errors.Is / errors.As, not shown to clients)
return response.Wrap(response.Internal, "failed_to_save_user", err)
```

| `ErrorCode` | HTTP status |
|---|---|
| `BadRequest` | 400 |
| `Unauthorized` | 401 |
| `Forbidden` | 403 |
| `NotFound` | 404 |
| `Conflict` | 409 |
| `UnsupportedMediaType` | 415 |
| `UnprocessableEntity` | 422 |
| `TooManyRequests` | 429 |
| `Internal` | 500 |

Predefined sentinels: `response.ErrInternalServer`, `response.ErrDatabaseConnection`.

### Handler example

```go
func (h *UserHandler) Get(c *gin.Context) {
	user, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		h.responder.Error(c, err) // ErrUserNotFound → 404 "User not found"
		return
	}
	h.responder.SendWithData(c, http.StatusOK, "operation_successful", user)
}
```

### Field filtering (`response/filter`)

Successful `SendWithData` / `Pagination` responses can be trimmed to the fields a client asks for.
The `filter` sub-package supports:

| Syntax | Example | Meaning |
|---|---|---|
| Dot notation | `data.items` | nested field |
| Index | `items[0]` | one array element |
| Projection | `items{id,name}` | only these keys of each element |

The `Responder` reads the selection from the gin context keys `_response_fields` and
`_response_omit` (`[]string`). Nothing in junkit sets them for you — populate them from the query
string in your own middleware if you want `?fields=` / `?omit=` support:

```go
r.Use(func(c *gin.Context) {
	if f := c.Query("fields"); f != "" {
		c.Set("_response_fields", strings.Split(f, ","))
	}
	if o := c.Query("omit"); o != "" {
		c.Set("_response_omit", strings.Split(o, ","))
	}
	c.Next()
})
```

The filter can also be used directly: `filter.Filter(data, []string{"id", "name"}, nil)`.

---

## 9. `validation`

Wraps [`go-playground/validator`](https://github.com/go-playground/validator), registers custom
rules on **Gin's binding validator** (so they work in `binding:"..."` tags), and converts binding
errors into **localized, field-keyed** messages.

### Custom rules

| Tag | Checks |
|---|---|
| `email_advanced` | Valid syntax **and** not a disposable domain **and** domain has live MX records (DNS lookup) |
| `password_strong` | ≥ 8 chars with lowercase, uppercase, digit and special character |
| `slug` | lowercase letters, digits, hyphens, dots |
| `alphanum_dash` | letters, digits, hyphens, underscores |
| `search` | letters, digits, spaces |
| `phone_advanced` | 7–15 digits, optional leading `+` |
| `date` | `YYYY-MM-DD` |
| `date_past` | a date in the past |
| `min_int` / `max_int` | integer bounds |
| `file_ext` | allowed file extensions, e.g. `file_ext=jpg png` |

All standard validator tags (`required`, `email`, `min`, `max`, `oneof`, `uuid`, `gte`, ...) keep
working and have English messages in the built-in `i18n` catalog.

### Usage

```go
type RegisterRequest struct {
	Email    string `json:"email"    binding:"required,email_advanced"`
	Password string `json:"password" binding:"required,password_strong"`
	Phone    string `json:"phone"    binding:"omitempty,phone_advanced"`
}

if err := c.ShouldBindJSON(&req); err != nil {
	h.responder.SendWithData(c, http.StatusUnprocessableEntity, "validation_error",
		h.validator.GetValidationErrors(c, err))
	return
}
```

`GetValidationErrors` returns a `map[field]message` for field errors, or a single message string
for errors not tied to a field (malformed JSON, wrong type, ...).

`BindJSON`, `BindQuery` and `ParseUUID` do the bind and the 422 response in one call; each returns
`false` once it has written the error response:

```go
if !validation.BindJSON(c, h.responder, h.validator, &req) { // BindQuery for query strings
	return
}

uid, ok := validation.ParseUUID(c, h.responder, h.validator) // the ":uuid" path parameter
if !ok {
	return
}
```

### Disposable-email blocklist

`email_advanced` checks domains against a blocklist loaded from
`validation.DefaultBlocklistPath` (`storage/bootstrap/email_blocklist.conf`, relative to the working
directory). If the file is missing it is **created from a built-in seed list** on startup. `go test`
runs each package in its own folder, so a test that calls `NewValidator` leaves a `storage/` folder
there — delete it, don't commit it. Edit the file (one domain per line) or load another one:

```go
_ = validation.LoadDisposableEmailsFromFile("config/blocklist.conf")

validation.IsDisposableEmail("mailinator.com") // true
validation.CheckMXRecord("example.com")        // DNS lookup
validation.ValidateEmail("a@example.com")      // all three checks
```

---

## 10. `pagination`

Standard list-endpoint query params → SQL `LIMIT`/`OFFSET` → response metadata. No Fx wiring.

```go
// GET /users?page=2&page_size=20&search=john&sort_by=created_at&sort_order=desc
var req pagination.Request         // embed it in your own request DTO to add filters
if err := c.ShouldBindQuery(&req); err != nil { /* 422 */ }
req.SetDefaults("created_at", "desc") // page=1, page_size=10, and the given sort when missing

rows, total, err := repo.List(ctx, req.Search, req.GetLimit(), req.GetOffset())

meta := pagination.NewPagination(req.Page, req.PageSize, total)
responder.Pagination(c, http.StatusOK, "operation_successful", rows, meta)
```

| `Request` field | Query param | Binding rule |
|---|---|---|
| `Page` | `page` | ≥ 1 |
| `PageSize` | `page_size` | 1–100 |
| `Search` | `search` | — |
| `SortBy` | `sort_by` | — (whitelist it yourself before using it in SQL!) |
| `SortOrder` | `sort_order` | `asc` / `desc` |

`Pagination` metadata: `page`, `page_size`, `total_items`, `total_pages`, `has_next`, `has_prev`,
`next_page`, `prev_page`. `PaginatedResponse[T]` is available if you need a typed
`{items, pagination}` value outside the `Responder`.

---

## 11. `database`

Two layers, so another engine can be added without touching the PostgreSQL code:

| Package | Contents |
|---|---|
| `database` | Driver-independent: error classification (`Classify`, `Match`, `MatchNotFound`, `IsNotFound`, ...), `RegisterClassifier`, `EscapeLike` (search term → literal inside `LIKE`/`ILIKE`, so `%` and `_` are not wildcards), `MaskDSN` |
| `database/postgres` | PostgreSQL via [pgx v5](https://github.com/jackc/pgx) `pgxpool`: connection pool, transactions, query logging, Postgres error classifier |

A future engine (e.g. `database/mysql`) is a new sibling package with its own pool, transactions and
classifier; repositories keep calling `database.Match` unchanged.

**Features (`database/postgres`)**

- Pool creation, ping on start, close on stop (Fx lifecycle hooks).
- `Executor(ctx)` returns the active **transaction** if one is on `ctx`, otherwise the pool — so
  repositories never need to know whether they run inside a transaction.
- `Executor(ctx)` is the only way to run a query: there is no raw-pool accessor, so every query —
  sqlc-generated or hand-written — is traced (`Decorate`) and joins the active transaction.
  `Ping(ctx)` covers health checks.
- `WithTransaction(ctx, fn)` commits on success, rolls back on error **or panic** (the panic is
  re-raised). The transaction is keyed to its own `*DB`, so with several databases one DB's
  `Executor` never picks up another's transaction.
- `DBTX` is compatible with [sqlc](https://sqlc.dev)-generated code (`sqlc.New(db.Executor(ctx))`).
- Optional structured **query logging** with slow-query escalation to `WARN`.
- `Decorate` hook to wrap every executor (tracing, metrics).
- Helpers: `BindArgsToSQL` / `FormatValue` (render SQL with arguments, for logs only).

**Options (`postgres.Options`)**

| Field | Meaning |
|---|---|
| `Name` | Identifies the database in logs (e.g. `"primary"`) |
| `DSN` | `postgres://user:pass@host:5432/db?sslmode=disable` |
| `MaxConnection` / `MinConnection` | Pool size bounds |
| `MaxConnLifetime` / `MaxConnIdleTime` | Connection recycling |
| `HealthCheckPeriod` | Idle connection health checks |
| `QueryLogging` | Log every query through the injected `*zap.Logger` |
| `SlowQueryThreshold` | With `QueryLogging`, queries slower than this log at `WARN` (0 = off) |
| `Decorate` | `func(DBTX) DBTX` wrapping every executor |

```go
fx.Supply(postgres.Options{
	DSN:                dsn,
	MaxConnection:      20,
	MinConnection:      2,
	QueryLogging:       true,
	SlowQueryThreshold: 200 * time.Millisecond,
}),
postgres.Module, // provides *postgres.DB (needs *zap.Logger)
```

`postgres.Module` provides a single `*postgres.DB`. An app with several databases skips it and calls
`postgres.NewDatabase(postgres.Params{...})` from one constructor per database, each returning its
own type (see jungo's `internal/database/primary`).

**Repository with error mapping**

```go
func (r *UserRepository) GetByUUID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	row, err := sqlc.New(r.db.Executor(ctx)).GetUserByUUID(ctx, id)
	if err != nil {
		return nil, database.MatchNotFound(err, domain.ErrUserNotFound)
	}
	return mapUser(row), nil
}
```

**Hand-written SQL** goes through the same executor, so it is traced and transactional too:

```go
rows, err := r.db.Executor(ctx).Query(ctx, `SELECT id, email FROM users WHERE status = $1`, status)
if err != nil {
	return nil, err
}
users, err := pgx.CollectRows(rows, pgx.RowToStructByName[User])
```

`Match` classifies the error and returns the mapped domain error; if the class isn't in the map it
returns the **original error unchanged** (which `responder.Error` then answers with a generic 500).

| `ErrorType` | Typical cause |
|---|---|
| `ErrorNotFound` | `pgx.ErrNoRows` |
| `ErrorUniqueViolation` | duplicate key |
| `ErrorForeignKey` | FK violation |
| `ErrorCheckViolation` | CHECK constraint |
| `ErrorConnection` | connectivity / timeout |
| `ErrorInvalidInput` | bad input syntax / data exceptions |
| `ErrorPermissionDenied` | insufficient privilege |
| `ErrorBusiness` | errors raised by your SQL functions (`RAISE EXCEPTION`) |
| `ErrorUnknown` | everything else |

`Classify` asks every engine classifier registered with `database.RegisterClassifier`;
`database/postgres` registers its own in `init`, so importing it is enough.

**Transactions**

```go
err := db.WithTransaction(ctx, func(txCtx context.Context) error {
	if err := userRepo.Create(txCtx, user); err != nil {
		return err // → rollback
	}
	return auditRepo.Create(txCtx, entry) // same tx, via db.Executor(txCtx)
})
```

A transaction covers one database only. Low level: `postgres.NewDB(pool)` builds a `*DB` from an
existing pool (e.g. in tests).

**Adding an engine** — a new `database/<engine>` package must:

1. Run queries only through `Executor(ctx)` and expose no raw pool.
2. Key its transaction in `ctx` to its own `*DB`.
3. Register a `database.Classifier` in `init`.
4. Wrap its executor with a tracer wrapper built on `tracer.BeginQuery` / `Finish` (see [20](#20-tracer)).
5. Pass a tracer contract test like `database/postgres`'s `TestExecutor_TracesEveryQuery`: `Exec`,
   `Query` and `QueryRow` through `Executor`, inside and outside a transaction, all reach the `Debugger`.

---

## 12. `cache`

One generic interface, three backends:

```go
type Cache[T any] interface {
	Get(ctx, key) (T, bool)
	Set(ctx, key, value T, ttl) error           // ttl 0 = no expiry
	Delete(ctx, key) error
	Exists(ctx, key) (bool, error)
	Incr(ctx, key, ttl) (int64, error)          // atomic +1; ttl applied only when creating the key
	GetOrSet(ctx, key, ttl, fetchFn) (T, error) // cache-aside in one call
	Close() error
}
```

| `Driver` | Implementation | Notes |
|---|---|---|
| `DriverRedis` | `RedisCache[T]` | Values JSON-encoded. Extras: `MGet`, `MSet`, `SetNX` (locks), `IncrBy`, `Expire`, `DeleteByPattern`, `Clear`, `Ping`, `GetClient` |
| `DriverMemory` | `MemoryCache[T]` | Go map; expired entries swept on writes. Single-process only |
| `DriverNoop` (default) | `NoopCache[T]` | Every read misses, writes are ignored, `GetOrSet` always calls `fetchFn` — turn caching off without touching code |

`Incr` only works on `Cache[int64]` or `Cache[any]` (otherwise `ErrNotInteger`) — handy for
fixed-window counters.

`opts.OrMemory()` keeps Redis but turns any other driver into `DriverMemory` — for counters and
blocks that must work without Redis, since a no-op cache would never block.

**`Invalidatable[T]`** caches values that must be dropped before their TTL when the data behind them
changes (sessions, permissions). `Invalidate(ctx, marker)` stores the time under a marker;
`GetOrLoad(ctx, key, markers, load)` treats an entry as a miss when one of `markers(value)` was set
after the entry's load *started* (plus a 2 s clock-skew margin), so a load that read old data and
finished after the change is not served. Markers live 2× TTL; `ttl <= 0` disables caching.

```go
sessions := cache.NewInvalidatable[*Session](opts, 3*time.Minute)
s, hit, err := sessions.GetOrLoad(ctx, "session:"+hash,
	func(s *Session) []string { return []string{"family:" + s.Family} },
	func() (*Session, error) { return repo.GetSession(ctx, hash) })
_ = sessions.Invalidate(ctx, "family:"+family) // on logout / revoke
```

**Via Fx** — `cache.Module` provides a `Cache[any]`:

```go
fx.Supply(cache.Options{
	Driver: cache.DriverRedis,
	Redis:  cache.RedisOptions{Addr: "localhost:6379", Password: "", DB: 0},
}),
cache.Module,
```

**Typed cache** — build one per type with `cache.New[T]`:

```go
users := cache.New[*domain.User](opts)

user, err := users.GetOrSet(ctx, "user:"+id.String(), 5*time.Minute, func() (*domain.User, error) {
	return repo.GetByUUID(ctx, id)
})
```

**Distributed lock (Redis only)**

```go
rc := cache.NewRedisCache[bool](cache.RedisOptions{Addr: "localhost:6379"})
if locked, _ := rc.SetNX(ctx, "lock:order:456", true, 30*time.Second); locked {
	defer rc.Delete(ctx, "lock:order:456")
	// ...
}
```

`RedisOptions` defaults: `PoolSize` 10×GOMAXPROCS, `MinIdleConns` 5, `DialTimeout` 5s,
`ReadTimeout`/`WriteTimeout` 3s. Pass a `Logger` (anything with
`Error(msg string, keysAndValues ...any)`) to see non-fatal Redis/marshal errors.

---

## 13. `storage`

File upload/delete behind a small interface, so you can swap the local implementation for S3/GCS
later without touching services.

```go
type Service interface {
	UploadFile(ctx, file io.Reader, originalFilename string) (url string, err error)
	DeleteFile(ctx, fileURL string) error
}
```

The shipped `LocalService` writes to `BaseDir` under a **generated unique filename** (keeping the
extension) and returns `BaseURL + "/" + name`. Deleting a file that no longer exists is not an
error.

```go
fx.Supply(storage.Options{BaseDir: "./uploads", BaseURL: "http://localhost:8080/uploads"}),
storage.Module, // provides storage.Service

// serve the files
r.Static("/uploads", "./uploads")

// in a service
url, err := s.storage.UploadFile(ctx, file, header.Filename)
_ = s.storage.DeleteFile(ctx, oldURL)
```

**Check the file first — `upload`.** `storage` stores any bytes under any extension, and `/uploads`
is public on the API origin, so an uploaded `.html` or `.svg` would run script there. `upload.Open`
refuses (`upload.ErrInvalidFile`) a file over `Rule.MaxSize`, with an extension outside
`Rule.Extensions`, or whose first 512 bytes are not one of `Rule.Types` (e.g. PHP renamed to `.png`),
then rewinds it so the whole file is stored:

```go
file, err := upload.Open(header, upload.Image(5<<20)) // JPEG, PNG, WebP up to 5 MB
if err != nil {
	return // 400
}
defer file.Close()

url, err := s.storage.UploadFile(ctx, file, header.Filename)
```

For other files write a `upload.Rule{MaxSize, Extensions, Types}`; `upload.HasType` and
`upload.Extension` are the two checks on their own.

---

## 14. `httpclient`

An `*http.Client` with sane production defaults and optional retries with exponential backoff.

| Option | Default |
|---|---|
| `Timeout` (whole request) | 15s |
| `DialTimeout` | 5s |
| `TLSHandshakeTimeout` | 5s |
| `ResponseHeaderTimeout` | 10s |
| `MaxIdleConns` / `MaxIdleConnsPerHost` | 100 / 20 |
| `IdleConnTimeout` | 90s |
| `MaxRetries` | 0 (no retry) |
| `RetryWaitMin` / `RetryWaitMax` | 200ms / 5s |

```go
fx.Supply(httpclient.Options{MaxRetries: 2}),
httpclient.Module, // provides *http.Client — used by telegram and recaptcha

// or directly
client := httpclient.NewClient(httpclient.Options{MaxRetries: 3, Timeout: 5 * time.Second})
client := httpclient.NewDefaultClient()
```

---

## 15. `telegram`

Minimal Telegram Bot API client. Messages use HTML parse mode.

```go
type Client interface {
	SendMessage(ctx, chatID, message string) error
	SendDocument(ctx, chatID string, content io.Reader, filename, caption string) error
}
```

- An empty `Token` or `chatID` (or nil `content`) logs and returns `nil` — so leaving Telegram
  unconfigured in development never breaks anything.
- `SendDocument` **streams** the reader, memory use stays constant regardless of file size.
- A nil `*http.Client` falls back to one with a 10s timeout.

```go
fx.Supply(telegram.Options{Token: os.Getenv("TELEGRAM_BOT_TOKEN")}),
httpclient.Module,
telegram.Module,

_ = tg.SendMessage(ctx, chatID, "<b>Deploy finished</b> ✅")
_ = tg.SendDocument(ctx, chatID, bytes.NewReader(report), "report.csv", "Daily report")
```

---

## 16. `notification`

Operational alerts to a Telegram chat, built on `telegram.Client`. Best-effort: send errors are
dropped, never returned to the request.

| Method | Behaviour |
|---|---|
| `SendMessageHTML(c, message, type)` | Prefixes an error banner when `type == "error"`, a warning banner otherwise |
| `SendErrorNotification(c, errData, status, sql...)` | Reports a failed request: location, a **repro `curl` command** (method, path and headers; secret header values masked, query string and body left out), optional SQL; attaches `errData["stack"]` as a document. **Only sends when `Environment == "production"`** |

Used automatically by `middleware.Recover` (panics), `middleware.Limiter` (rate-limit breaches) and
`middleware.Banner` (new bans).

```go
fx.Supply(notification.Options{TelegramChatID: chatID, Environment: "production"}),
telegram.Module,
notification.Module,

notifier.SendMessageHTML(c, "<b>Payment provider is slow</b>", "warning")
```

---

## 17. `recaptcha`

Google reCAPTCHA v3 verification.

```go
type Verifier interface {
	Verify(ctx context.Context, token, remoteIP string) error // nil = accepted and score ≥ MinScore
}
```

| Option | Meaning |
|---|---|
| `SecretKey` | Your site's secret key |
| `MinScore` | 0.0–1.0, defaults to 0.5 |
| `Mock` | `true` → `MockVerifier` that accepts every token (local dev, tests) |

```go
fx.Supply(recaptcha.Options{
	SecretKey: os.Getenv("RECAPTCHA_SECRET"),
	Mock:      env != "production",
}),
httpclient.Module,
recaptcha.Module, // provides recaptcha.Verifier (real or mock)

if err := verifier.Verify(ctx, req.RecaptchaToken, c.ClientIP()); err != nil {
	responder.Error(c, err)
	return
}
```

---

## 18. `security`

Building blocks for authentication. No user/session model — that belongs to your app.

### Opaque tokens: generate → hash → seal

The recommended flow for access/refresh tokens:

```go
raw := security.GenerateToken()   // 32 random bytes, base64url
hash := security.HashToken(raw)   // hex SHA-256 → store THIS in the DB
token, _ := sealer.Seal(raw)      // AES-256-GCM + HMAC-SHA256 → return THIS to the client

// later, on an incoming request
raw, err := sealer.Open(token)    // ErrMalformedToken | ErrBadSignature | ErrDecrypt
session, err := repo.FindByHash(ctx, security.HashToken(raw))
```

A DB leak exposes only hashes; a stolen sealed token is useless without the server's keys. Sealed
format: `v1.<nonce+ciphertext>.<signature>`; the signature is verified **before** decrypting.

### Passwords (bcrypt)

```go
hash, err := hasher.Hash(plain)  // ErrPasswordTooLong if > 72 bytes (no silent truncation)
ok := hasher.Compare(hash, plain)
hasher.CompareDummy(plain)       // call when the user does NOT exist → same timing, no account enumeration
```

### Identity in context

```go
security.SetIdentity(c, identity)                        // done by your app's auth middleware (see 26)
id := security.MustGetIdentity[*domain.Identity](c)      // in handlers behind auth (panics if missing)
id, ok := security.GetIdentity[*domain.Identity](ctx)    // works with *gin.Context or context.Context
ctx := security.WithIdentity(context.Background(), id)   // tests / background jobs
```

### Other helpers

`SecureEqual(a, b)` (constant-time compare), `RandomBytes(n)`, `DecodeKey(s)` (base64, standard or
URL alphabet, padded or not), `ClientKey(ip)` (the key to rate-limit or match a client by: an IPv6
address becomes its `/64` block, so rotating addresses inside it does not reset a limit).

### Wiring

```go
hmacKey, _ := security.DecodeKey(os.Getenv("AUTH_TOKEN_HMAC_SECRET"))   // ≥ 32 bytes
encKey, _ := security.DecodeKey(os.Getenv("AUTH_TOKEN_ENCRYPTION_KEY")) // exactly 32 bytes

fx.Supply(security.Options{
	TokenHMACSecret:    hmacKey,
	TokenEncryptionKey: encKey,
	Password:           security.PasswordOptions{Cost: 12}, // 0 → bcrypt.DefaultCost
	// MaxConcurrent caps bcrypt calls running at once (default GOMAXPROCS/2), so a login flood
	// cannot take every core from the other routes; the rest wait their turn.
}),
security.Module, // provides TokenSealer + PasswordHasher
```

Generate keys with `openssl rand -base64 32`. Wrong key sizes fail at startup with
`ErrInvalidKey`; an out-of-range cost with `ErrInvalidPasswordCost`.

---

## 19. `middleware`

Gin middleware. Not an Fx module — call the constructors where you register routes, so each group
can have its own configuration.

junkit ships only the middleware every app needs to stay safe and easy to build on — security
headers, tracing, CORS, body limits, rate limiting, panic recovery, access logs, route patterns and
the debug switch. **Authentication and authorization are app policy and live in the app** (Jungo:
`internal/middleware` — `BearerAuth`, `RequireSecret` and the permission `Guard`), because every
app decides differently who may call what. See [26](#26-bearer-token-authentication).

| Middleware | What it does |
|---|---|
| `Security()` | Sets HSTS, `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy`, `Cross-Origin-Opener-Policy`, `Cross-Origin-Resource-Policy`, `Permissions-Policy` |
| `Trace(TraceOptions)` | Reuses the incoming `X-Trace-Id` when it is 1–64 letters, digits, `-` or `_`, otherwise generates a UUID; puts it in the request context (`logger.GetTraceID`) and the response header; stores start time, version, env for `response.APIInfo` |
| `CORS(CORSOptions)` | Validates origins, answers preflights. With `AllowCredentials` and `"*"`, echoes the request origin (browsers reject `*` with credentials) |
| `Payload(PayloadOptions, responder)` | Reads and caches the request body (JSON or form) once, enforcing `MaxBodySize` (→ 413), or a larger per-route limit from `RouteLimits` (keyed by `RouteKey(method, pattern)`). Read it with `GetPayload(c)`; raw bytes under `PayloadRawKey` |
| `Limiter(LimiterOptions, log, responder, notifier)` | Per-client token-bucket rate limiting (→ 429; an IPv6 client is its `/64`, see `security.ClientKey`), sharded maps, idle-client eviction, IP whitelist, throttled logging/alerts |
| `Banner(BannerOptions, log, responder, notifier)` | Bans clients probing the API (→ 403 `access_denied` + `Retry-After`): one request bans at once when `TrapPaths` match its path, if it matched no route (`.env` files such as `/.env.example` or `/production.env`, `/.git`, `.php`, `/wp-admin`, VPN/appliance panels, `../`, ... at any depth), `AttackPatterns` its decoded query (Log4Shell, SQL/command injection, XSS, SSRF to cloud metadata) or `ScannerAgents` its User-Agent (sqlmap, nuclei, ffuf, ...), `MaxStrikes` 401/403/404/405/429 responses within `StrikeWindow` ban too; repeat bans escalate through `BanDurations`. Keyed by `security.ClientKey`, state in a `cache.Cache[int64]` (Redis shares bans across instances), one log/alert per ban. Place it before `Limiter` |
| `Recover(log, notifier, responder, RecoverOptions)` | Catches panics → logs stack, alerts via `notifier`, responds 500. Lets `tracer.Stop` breakpoints through |
| `AccessLog(httpLogger, AccessLogOptions)` | Structured request/response log with sensitive-field masking (body fields and query parameters; any name containing password, token, secret, api key or authorization is always masked) |
| `RoutePatterns(patterns, responder)` | Laravel-style `Route::pattern`: a path param named in `patterns` that fails its rule → 404 `api_not_found`, before any route or group middleware. Rules: `IsUUID` (canonical 8-4-4-4-12), `IsID` (positive int64), `IsSlug` (`a-z0-9` joined by single `-`) |
| `TracerDebug(TracerDebugOptions)` | Turns on the [`tracer`](#20-tracer) dashboard for requests carrying `?<QueryKey>=<QueryValue>` and the `SecretHeader` set to `Secret` (constant-time compare; any empty field disables it) |

Helpers: `GetPayload(c)` returns the cached body; `RouteKey(method, pattern)` builds a `RouteLimits` key.

### Options at a glance

```go
middleware.CORSOptions{
	AllowOrigins:     []string{"https://app.example.com"}, // or "*"
	AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE"},
	AllowHeaders:     []string{"Authorization", "Content-Type"},
	AllowCredentials: true,
	MaxAge:           600,
}

middleware.LimiterOptions{
	RequestsPerSecond: 10,
	Burst:             30,
	CleanupInterval:   time.Minute,      // sweep idle clients
	ClientTTL:         10 * time.Minute, // evict after this long idle
	LogTTL:            time.Minute,      // ≤ 1 log/alert per client per minute
	NumShards:         64,
	IPWhitelist:       []string{"127.0.0.1"},
}

middleware.BannerOptions{
	Cache:        cache.New[int64](cacheOpts), // Redis or memory; a no-op cache disables banning
	MaxStrikes:   30,                          // strike responses...
	StrikeWindow: time.Minute,                 // ...within this window ban the client
	BanDurations: []time.Duration{15 * time.Minute, time.Hour, 24 * time.Hour}, // 1st, 2nd, 3rd+ ban
	IPWhitelist:  []string{"127.0.0.1"},
	// TrapPaths / AttackPatterns / ScannerAgents / StrikeStatuses default to the Default* values.
	// Patterns see lowercased input; start each with a literal so it stays fast.
	// Add one: append(middleware.DefaultTrapPaths, regexp.MustCompile(`/my-trap`));
	// turn a check off: []*regexp.Regexp{}.
}

middleware.AccessLogOptions{
	ExcludedPaths:     []string{"/health", "/metrics"}, // default: /health, /metrics, /favicon.ico
	SensitiveFields:   []string{"password", "token"},   // default: common credential fields
	BodiesOnErrorOnly: true,                            // log payloads only when status >= 400
	MaxBodySize:       100 << 10,                       // response bytes buffered for logging
	SkipIf:            func(c *gin.Context) bool { return c.Request.Method == http.MethodOptions },
}

middleware.TracerDebugOptions{QueryKey: "t_debug", QueryValue: "0604", SecretHeader: "X-Internal-Secret", Secret: secret}
middleware.RecoverOptions{Debug: debugOpts} // same TracerDebugOptions as TracerDebug
middleware.PayloadOptions{
	MaxBodySize: 2 << 20, // 2 MB for every route...
	// ...except these: a global Payload runs before route middleware, so a route's larger limit lives here.
	RouteLimits: map[string]int64{middleware.RouteKey(http.MethodPost, "/api/v1/users/:uuid/avatar"): 6 << 20},
}
middleware.TraceOptions{Version: "v1.2.0", Commit: "abc123", Environment: "production"}
```

### Per-group configuration

```go
api := r.Group("/api/v1")

auth := api.Group("/auth",
	middleware.Limiter(middleware.LimiterOptions{RequestsPerSecond: 1, Burst: 5}, log, responder, notifier))
```

See [24](#24-recommended-middleware-chain) for the recommended global order.

---

## 20. `tracer`

A request-scoped debug recorder. Add `?<key>=<value>` to any request and send the secret header
(both configured by `middleware.TracerDebugOptions`) and, instead of the normal response, you get a dashboard of everything
that happened during that request:

- every **SQL query** (bound SQL, duration, returned rows / rows affected, errors with the calling
  code location), plus a summary: query count, total DB time, % of request time, **repeated
  queries (possible N+1)**;
- your own **notes, variables, warnings and errors** (with stack traces);
- a **timeline / waterfall** of spans by category;
- request and server info.

Browsers get an interactive **HTML** page; `curl`, `httpie` and `wget` get **JSON**.

**Without an active debugger every call is a cheap no-op**, so instrumentation can stay in
production code permanently.

| Function | Records |
|---|---|
| `C(ctx, msg)` | a comment |
| `V(ctx, label, values...)` | variables |
| `W(ctx, label, err)` | a warning, with source location |
| `E(ctx, label, err)` | an error, with location + full stack |
| `Span(ctx, label)` → `func()` | timing of a block (`defer tracer.Span(ctx, "x")()`) |
| `Measure(ctx, label, fn)` | timing of a closure |
| `Stop(ctx, label, values...)` | records values then **halts the request** (a breakpoint) |
| `Enabled(ctx)` | whether debugging is on — skip expensive debug-only work otherwise |

```go
func (s *OrderService) Checkout(ctx context.Context, order Order) error {
	defer tracer.Span(ctx, "Checkout")()

	tracer.V(ctx, "order", order)

	tracer.Measure(ctx, "Charge payment provider", func() {
		err = s.payments.Charge(ctx, order.Total)
	})
	if err != nil {
		tracer.E(ctx, "charge failed", err)
		return err
	}

	tracer.Stop(ctx, "state before save", order) // dashboard shows state at this point
	// ...
}
```

> `tracer.Stop` panics with `tracer.BreakpointSignal`, which `TracerDebug` recovers. Only call it
> from the request's own goroutine — a panic in another goroutine crashes the process.

**SQL capture** is enabled once, through the `postgres.Options.Decorate` hook
(`tracer.NewPgxWrapper`) — see [2](#core-packages-never-import-optional-ones). It sits on the
executor, not on sqlc, so hand-written SQL is captured too.

Other drivers get the same capture by wrapping their executor with `BeginQuery` / `Finish`; `bind`
renders arguments for display and must match the driver's placeholders:

```go
q := tracer.BeginQuery(ctx, sql, args, sqlfmt.BindQuestion) // nil when the request isn't debugged
if q == nil {
	return inner.ExecContext(ctx, sql, args...)
}
res, err := inner.ExecContext(ctx, sql, args...)
q.Finish(tracer.SQLData{}, err)
```

Lower-level API (`New`, `WithContext`, `FromContext`, `NewReport`, `RenderHTML`, `RenderJSON`) is
available if you want to embed the debugger elsewhere (e.g. in a console command).

> **Security:** the dashboard exposes SQL, data and server details. Use a long random `value`, or
> leave key/value empty in production to disable it entirely.

---

## 21. `sqlfmt`

Small helpers shared by `database/postgres` (query logging) and `tracer` (dashboard). Useful if you build
your own SQL logging.

```go
info := sqlfmt.Parse(sql)            // Name (from "-- name: GetUser :one"), Operation ("ONE"), compact SQL
s := sqlfmt.Bind(sql, args)          // "$1" → literal values, for logs only
s = sqlfmt.BindQuestion(sql, args)   // "?" → literal values (MySQL, ClickHouse, SQLite)
lit := sqlfmt.FormatValue("O'Brien") // "'O''Brien'"
loc, fn := sqlfmt.Origin()           // "file:line" + function of the app code that ran the query (skips *.sql.go)
```

---

## 22. `scaffold`

The engine behind a project's `make feature NAME=...` generator: name-form derivation, templated
file rendering (`text/template`) and automatic Fx module registration. **Templates live in your
app**, so generated code matches your own layering.

```go
data := scaffold.NewFeatureData("product_category", "github.com/acme/api", "")
// Pascal "ProductCategory", Camel "productCategory", Table "product_categories"

seq, _ := scaffold.NextMigrationSeq("internal/database/primary/migrations") // "000007"

err := scaffold.Generate(scaffold.GenerateConfig{
	Data: data,
	Files: []scaffold.File{
		{Path: "internal/features/product_category/module.go", Template: moduleTmpl},
		{Path: "internal/database/primary/migrations/" + seq + "_create_product_categories.up.sql", Template: upTmpl},
	},
	FxFile:     "internal/app/fx.go",
	ImportPath: "github.com/acme/api/internal/features/product_category",
	Marker:     "// scaffold:modules", // ModuleLine is inserted after this comment line
	ModuleLine: "product_category.Module,",
})

// undo
err = scaffold.Remove(scaffold.RemoveConfig{
	Dirs:       []string{"internal/features/product_category"},
	ExtraFiles: []string{"internal/database/primary/queries/product_category.sql"},
	FxFile:     "internal/app/fx.go",
	ImportPath: "github.com/acme/api/internal/features/product_category",
	ModuleLine: "product_category.Module,",
})
```

---

# Part III — Recipes

## 23. Full Fx wiring

All packages together. In a real app, build the `Options` values from your own config struct
(see `jungo`'s `internal/app/fx.go`).

```go
fx.New(
	fx.Supply(
		logger.Options{Environment: env, Level: "info", Filename: "logs/app.log", MaxSize: 10},
		response.Options{Language: i18n.LangEN},
		validation.Options{Language: i18n.LangEN},
		postgres.Options{
			DSN:           dsn,
			MaxConnection: 20,
			QueryLogging:  env == "development",
			Decorate:      func(in postgres.DBTX) postgres.DBTX { return tracer.NewPgxWrapper(in) },
		},
		cache.Options{Driver: cache.DriverRedis, Redis: cache.RedisOptions{Addr: redisAddr}},
		storage.Options{BaseDir: "./uploads", BaseURL: publicURL + "/uploads"},
		httpclient.Options{MaxRetries: 2},
		telegram.Options{Token: tgToken},
		notification.Options{TelegramChatID: tgChat, Environment: env},
		recaptcha.Options{SecretKey: recaptchaSecret, Mock: env != "production"},
		security.Options{TokenHMACSecret: hmacKey, TokenEncryptionKey: encKey},
	),

	// core
	logger.Module,
	i18n.Module,
	response.Module,
	validation.Module,
	postgres.Module,

	// optional — remove any line (and its Options) you don't need
	cache.Module,
	storage.Module,
	httpclient.Module,
	telegram.Module,     // needs httpclient
	notification.Module, // needs telegram
	recaptcha.Module,    // needs httpclient
	security.Module,

	// your features + HTTP server
	fx.Invoke(startServer),
).Run()
```

---

## 24. Recommended middleware chain

```go
r := gin.New()
r.Use(
	middleware.TracerDebug(debugOpts), // outermost: captures everything below
	middleware.Security(),
	middleware.Trace(middleware.TraceOptions{Version: cfg.Version, Environment: cfg.Env}),
	middleware.AccessLog(httpLogger, middleware.AccessLogOptions{}),  // after Trace → has trace ID
	middleware.CORS(middleware.CORSOptions{AllowOrigins: cfg.AllowOrigins}),
	middleware.Payload(middleware.PayloadOptions{MaxBodySize: 2 << 20}, responder),
	middleware.Banner(middleware.BannerOptions{Cache: banCache, MaxStrikes: 30, StrikeWindow: time.Minute, BanDurations: banDurations}, log, responder, notifier),
	middleware.Limiter(middleware.LimiterOptions{RequestsPerSecond: 10, Burst: 30}, log, responder, notifier),
	middleware.RoutePatterns(map[string]func(string) bool{"uuid": middleware.IsUUID, "id": middleware.IsID, "slug": middleware.IsSlug}, responder),
	middleware.Recover(log, notifier, responder, middleware.RecoverOptions{Debug: debugOpts}),
)
```

Why this order:

- `TracerDebug` first so the dashboard wraps the whole chain and can replace the response.
- `Trace` before anything that logs or responds, so the trace ID and `api_info` are available.
- `Payload` early so oversized bodies are rejected (413) before any further work.
- `Banner` before `Limiter`, so a banned client is refused without using up rate-limit tokens and
  the 429s `Limiter` returns count as strikes.
- `RoutePatterns` after `Limiter` (junk URLs still count toward the rate limit) and, being global,
  before every group middleware, such as the app's auth middleware.
- `Recover` last (innermost) so it catches panics from handlers while every other middleware has
  already run.

---

## 25. Repository + transaction + error mapping

```go
// domain/errors.go — errors carry an HTTP-mapped code and an i18n key
var (
	ErrUserNotFound       = response.New(response.NotFound, "user_not_found")
	ErrEmailAlreadyExists = response.New(response.Conflict, "email_already_exists")
)

// repository — map driver errors to domain errors at the boundary
func (r *UserRepository) Create(ctx context.Context, in CreateUser) (*User, error) {
	row, err := sqlc.New(r.db.Executor(ctx)).CreateUser(ctx, in.toParams())
	if err != nil {
		return nil, database.Match(err, map[database.ErrorType]error{
			database.ErrorUniqueViolation: ErrEmailAlreadyExists,
		})
	}
	return mapUser(row), nil
}

// service — group writes atomically
func (s *UserService) Register(ctx context.Context, in CreateUser) (*User, error) {
	var user *User
	err := s.db.WithTransaction(ctx, func(txCtx context.Context) error {
		var err error
		if user, err = s.users.Create(txCtx, in); err != nil {
			return err
		}
		return s.audit.Log(txCtx, "user.registered", user.ID)
	})
	return user, err
}

// handler — one line turns any of the above into the right HTTP response
if err != nil {
	h.responder.Error(c, err) // 409 "Email already exists", 404 ..., or a generic 500
	return
}
```

---

## 26. Bearer-token authentication

junkit gives you the pieces — `security.TokenSealer`, `security.HashToken`, the identity context
helpers — and the app writes the middleware, because who may call what is app policy. Jungo's
`internal/middleware/auth.go` is a ready-made version (`BearerAuth`, `BearerToken`,
`RequireSecret`) you can copy. Its core:

```go
// in your app, e.g. internal/middleware/auth.go
func BearerAuth(authenticate func(ctx context.Context, token string) (*Identity, error), responder response.Responder) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if len(header) < 7 || !strings.EqualFold(header[:7], "bearer ") {
			responder.Send(c, http.StatusUnauthorized, "unauthorized_token")
			c.Abort()
			return
		}

		identity, err := authenticate(c.Request.Context(), strings.TrimSpace(header[7:]))
		if err != nil {
			responder.Error(c, err) // *response.AppError → its status; anything else → 500
			c.Abort()
			return
		}

		security.SetIdentity(c, identity) // handlers: security.MustGetIdentity[*Identity](c)
		c.Next()
	}
}

// authenticate: open the sealed token, look the session up by hash
authenticate := func(ctx context.Context, token string) (*Identity, error) {
	raw, err := sealer.Open(token)
	if err != nil {
		return nil, response.New(response.Unauthorized, "unauthorized_token")
	}
	session, err := sessions.FindByHash(ctx, security.HashToken(raw))
	if err != nil {
		return nil, response.New(response.Unauthorized, "unauthorized_token")
	}
	return &Identity{UserID: session.UserID}, nil
}
```

For service-to-service routes, compare a shared secret with `security.SecureEqual` (constant time)
and deny everything while the secret is empty. The i18n keys `unauthorized_token`,
`forbidden_internal_access` and `insufficient_permission` are built in.

Login side, with timing-safe user lookup:

```go
user, err := repo.FindByEmail(ctx, email)
if err != nil {
	hasher.CompareDummy(password) // same cost as a real compare
	return response.New(response.Unauthorized, "invalid_credentials")
}
if !hasher.Compare(user.PasswordHash, password) {
	return response.New(response.Unauthorized, "invalid_credentials")
}
raw := security.GenerateToken()
_ = sessions.Create(ctx, user.ID, security.HashToken(raw))
token, _ := sealer.Seal(raw)
```

---

## 27. Debugging a request with `tracer`

With `middleware.TracerDebug(middleware.TracerDebugOptions{QueryKey: "t_debug", QueryValue: "0604",
SecretHeader: "X-Internal-Secret", Secret: "my-secret"})` installed and the `Decorate` hook set on
`postgres.Options`:

```bash
# browser: open the interactive dashboard (add the header with an extension such as ModHeader)
open "http://localhost:8080/api/v1/users?t_debug=0604"

# terminal: JSON report
curl -s "http://localhost:8080/api/v1/users?t_debug=0604" -H "X-Internal-Secret: my-secret" | jq '.db'
```

```json
{
  "queries": 12,
  "failed": 0,
  "total_ms": 8.4,
  "percent": 61.2,
  "repeated": [{ "name": "GetRoleByID", "count": 10, "total_ms": 5.1 }]
}
```

`repeated` above is a classic N+1 — fetch the roles in one query instead. Add `tracer.V`/`Span`/
`Stop` calls in the code path you are investigating to see more.

---

# Part IV — Contributing

## 28. Development

```bash
go build ./...                  # build every package
go test ./...                   # run all tests
go test -race ./...             # with the race detector
go test ./cache/... -run Memory # one package / one test
go vet ./...
go doc -all ./middleware        # read a package's full API docs
```

Layout: one directory per package, flat, no `internal/`. Each package follows the same shape:

```
<package>/
├── <package>.go   # public types, Options, constructors
├── module.go      # var Module = fx.Module(...)   (when the package has one)
├── *.go           # implementation, split by concern
└── *_test.go      # black-box tests (package <name>_test) + *_internal_test.go when needed
```

Conventions when adding or changing a package:

- Configuration via an `Options` struct only — never read env vars or files implicitly.
- Expose a `Module` when the package provides something injectable; keep constructors exported.
- Depend on interfaces across packages; core packages (`logger`, `i18n`, `response`,
  `validation`, `database`) must not import optional ones — add an extension point instead.
- Every exported symbol gets a GoDoc comment, with a `Usage:` example for constructors and main
  functions.
- New user-facing message keys go into the built-in English catalog in `i18n/i18n.go`.
