package main

import (
	"log"
	"os"
	"strconv"
)

// debugLogging gates per-resource diagnostics, which would otherwise print several lines for
// every resource in a scan. go-plugin starts the plugin with the host's environment, so
// CORKSCREW_DEBUG=1 on the corkscrew command reaches this process.
var debugLogging = debugEnabled(os.Getenv("CORKSCREW_DEBUG"))

// debugEnabled reports whether value is a true boolean ("1", "true", ...). Anything else,
// including empty or malformed values, leaves debug logging off.
func debugEnabled(value string) bool {
	enabled, err := strconv.ParseBool(value)
	return err == nil && enabled
}

// debugLogf logs with a DEBUG prefix only when CORKSCREW_DEBUG is set.
func debugLogf(format string, args ...any) {
	if debugLogging {
		log.Printf("DEBUG: "+format, args...)
	}
}
