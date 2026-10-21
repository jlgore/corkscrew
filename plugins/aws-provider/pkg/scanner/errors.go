package scanner

import "fmt"

// AccessDeniedError is a type the credential is not permitted to list.
type AccessDeniedError struct {
	Type string
	Err  error
}

func (e *AccessDeniedError) Error() string {
	return fmt.Sprintf("access_denied:%s: %v", e.Type, e.Err)
}

func (e *AccessDeniedError) Unwrap() error { return e.Err }

// FlattenErrors returns the leaf errors of err, expanding errors.Join, so
// each failed type or account becomes its own entry in a scan's Errors
// instead of one entry with every failure on separate lines. A nil err
// yields nil.
//
// Any error with an Unwrap() []error method is expanded, so callers must not
// wrap several errors with one fmt.Errorf when the wrapper's text matters.
// The walk uses an explicit stack rather than recursion so a deep join tree
// cannot grow the goroutine stack.
func FlattenErrors(err error) []error {
	if err == nil {
		return nil
	}
	var leaves []error
	stack := []error{err}
	for len(stack) > 0 {
		e := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		joined, ok := e.(interface{ Unwrap() []error })
		if !ok {
			leaves = append(leaves, e)
			continue
		}
		children := joined.Unwrap()
		// Push in reverse so leaves come out in their original order.
		for i := len(children) - 1; i >= 0; i-- {
			if children[i] != nil {
				stack = append(stack, children[i])
			}
		}
	}
	return leaves
}
