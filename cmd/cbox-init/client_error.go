package main

import (
	"fmt"
	"os"
	"strings"
)

// connectionFailed reports whether err is a failure to reach the API at all
// (as opposed to the API responding with an error). apiclient wraps such
// failures with "failed to connect to API".
func connectionFailed(err error) bool {
	return err != nil && strings.Contains(err.Error(), "failed to connect to API")
}

// exitAPIError prints a failed API call to stderr and exits non-zero. When the
// failure is a connection problem it adds a hint about starting the daemon, so
// the user is not left with a bare dial error. format/args describe the action,
// e.g. exitAPIError(err, "Failed to restart %s", name).
func exitAPIError(err error, format string, args ...any) {
	fmt.Fprintf(os.Stderr, "❌ "+format+": %v\n", append(args, err)...)
	if connectionFailed(err) {
		fmt.Fprintln(os.Stderr, "   The daemon may not be running. Start it with 'cbox-init serve', or point --url at the API endpoint.")
	}
	os.Exit(1)
}
