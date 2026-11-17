package recaptcha

import "errors"

var (
	// ErrVerificationFailed means Google rejected the token, or its action or hostname did not match.
	ErrVerificationFailed = errors.New("recaptcha: verification failed")

	// ErrLowScore means the token was valid but its score is below Options.MinScore.
	ErrLowScore = errors.New("recaptcha: score too low")

	// ErrUnavailable means the siteverify API could not be reached or returned an unusable answer;
	// callers may choose to fail open or closed on it.
	ErrUnavailable = errors.New("recaptcha: verification service unavailable")
)
