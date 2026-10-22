// Package assert checks invariants that only a programmer error can break.
//
// An assertion states something the code guarantees by construction: every scope gets a
// result, a counter never passes its bound, a destination was already made absolute. When
// one fails, the program is wrong and continuing would turn the bug into bad data (a scan
// claiming completeness for a region it never saw), so the assertion panics.
//
// Assertions are never compiled out or disabled. They are not for operating errors: bad user
// input, a malformed cloud response, a network failure or a missing permission is expected and
// must be returned as an error or recorded in the scan's Errors. If external data can make a
// condition false, it is an error, not an assertion. See docs/contributing/assertions.md.
//
// Plugins are separate modules that replace the core module with ../.., so they import this
// package too.
package assert

import "fmt"

// Prefix starts the text of every assertion failure, so a crash report is easy to tell apart
// from an operating error.
const Prefix = "assertion failed: "

// Failure is the panic value of a failed assertion. It implements error so recover sites that
// convert panics to errors keep the message.
type Failure struct {
	Message string
}

func (f Failure) Error() string {
	return Prefix + f.Message
}

// That panics with a Failure carrying message when condition is false. Write message as the
// property that should have held ("every scope has a result"), not as the failure.
func That(condition bool, message string) {
	if !condition {
		panic(Failure{Message: message})
	}
}

// Thatf is That with a formatted message. The arguments are evaluated on every call but only
// formatted on failure, so keep them cheap.
func Thatf(condition bool, format string, args ...any) {
	if !condition {
		panic(Failure{Message: fmt.Sprintf(format, args...)})
	}
}
