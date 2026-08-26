package schedule

import "errors"

// ErrJobNotFound means the named scheduled job does not exist. It is wrapped by
// the scheduler's lookups so callers (and the API) can classify a missing job
// with errors.Is instead of matching on the message.
var ErrJobNotFound = errors.New("job not found")
