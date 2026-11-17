// Package middleware provides Gin HTTP middleware: TracerDebug (debug dashboard), Security
// (security headers), Trace (trace IDs), CORS, Gzip, AccessLog, Recover (panic recovery), Banner
// (bans probing clients), Limiter (per-client rate limiting), Payload (body limits and caching) and
// RoutePatterns (path parameter rules), plus SetIdentity for auth middleware.
//
// Constructors panic on invalid static configuration, at startup rather than per request. Banner
// and Limiter key clients by c.ClientIP(), so the engine's trusted proxies must be set correctly:
// trusting a proxy that forwards client-supplied X-Forwarded-For lets clients pick their own IP.
package middleware
