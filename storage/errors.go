package storage

import "errors"

var (
	// ErrUnsafeExtension is returned by UploadFile for a missing extension or one a browser could
	// run as active content (HTML, SVG, scripts) when the file is served back.
	ErrUnsafeExtension = errors.New("storage: unsafe file extension")

	// ErrInvalidURL is returned by DeleteFile for a URL that does not name a file under BaseURL.
	ErrInvalidURL = errors.New("storage: invalid file url")
)
