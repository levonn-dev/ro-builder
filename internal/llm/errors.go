package llm

import "errors"

// IsAuthError reports whether err is, or wraps, an authentication or
// authorization failure (typically a missing / invalid / revoked API
// key). Concrete providers expose their auth errors via a type that
// implements `IsAuthError() bool`; this function uses errors.As to find
// it. Pattern matches Go stdlib conventions (net.Error, etc.).
//
// Generic enough that callers (handlers, retry logic) don't need to
// import any provider package; the test is on capability, not concrete
// type. Adding a new provider just means its error type implements the
// same method.
func IsAuthError(err error) bool {
	type authErr interface {
		IsAuthError() bool
	}
	var ae authErr
	if errors.As(err, &ae) {
		return ae.IsAuthError()
	}
	return false
}
