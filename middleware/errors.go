package middleware

import "github.com/jun-develop/junkit/response"

// Errors the middlewares answer with, exported so an app can list them in its API docs.
var (
	// ErrAccessDenied is Banner's answer to a banned client.
	ErrAccessDenied = response.New(response.Forbidden, "access_denied")
	// ErrTooManyRequests is Limiter's answer to a client over its rate.
	ErrTooManyRequests = response.New(response.TooManyRequests, "too_many_requests")
	// ErrAPINotFound is RoutePatterns' answer to a path parameter that breaks its rule.
	ErrAPINotFound = response.New(response.NotFound, "api_not_found")
	// ErrBodyTooLarge is Payload's answer to a body over its limit.
	ErrBodyTooLarge = response.New(response.PayloadTooLarge, "request_body_too_large")
	// ErrInvalidBody is Payload's answer to a body it cannot read.
	ErrInvalidBody = response.New(response.BadRequest, "invalid_request")
)
