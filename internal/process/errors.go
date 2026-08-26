package process

import "errors"

// Sentinel errors classify manager/supervisor failures so callers (the API in
// particular) can map them to the right outcome with errors.Is, instead of
// matching on message substrings — which silently turned a reworded message into
// the wrong HTTP status, and misclassified unrelated errors that merely happened
// to contain "not found" (e.g. `exec: "php-fpm": executable file not found`).
var (
	// ErrProcessNotFound means the named process does not exist.
	ErrProcessNotFound = errors.New("process not found")
	// ErrProcessExists means a process with that name already exists.
	ErrProcessExists = errors.New("process already exists")
	// ErrInvalidState means the process is not in a state that permits the
	// requested operation (e.g. starting an already-running process).
	ErrInvalidState = errors.New("process in invalid state for operation")
	// ErrInvalidArgument means the request itself was malformed (empty name,
	// empty command, and similar).
	ErrInvalidArgument = errors.New("invalid argument")
)
