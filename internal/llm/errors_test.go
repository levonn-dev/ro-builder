package llm

import (
	"errors"
	"fmt"
	"testing"
)

// fakeAuthError is a minimal implementation of the duck-typed interface
// IsAuthError(err) checks for. Used to verify the interface-based
// detection works without importing any concrete provider.
type fakeAuthError struct {
	msg  string
	auth bool
}

func (e *fakeAuthError) Error() string     { return e.msg }
func (e *fakeAuthError) IsAuthError() bool { return e.auth }

func TestIsAuthError_DirectAuthError(t *testing.T) {
	err := &fakeAuthError{msg: "bad key", auth: true}
	if !IsAuthError(err) {
		t.Error("expected auth error to be detected")
	}
}

func TestIsAuthError_NonAuthSubtype(t *testing.T) {
	// A type that implements the interface but reports false should not
	// be classified as an auth error; provider can return a single
	// concrete error type for both auth and non-auth failures.
	err := &fakeAuthError{msg: "rate limited", auth: false}
	if IsAuthError(err) {
		t.Error("non-auth error returning false should not be classified as auth")
	}
}

func TestIsAuthError_WrappedError(t *testing.T) {
	inner := &fakeAuthError{msg: "bad key", auth: true}
	wrapped := fmt.Errorf("transport: %w", inner)
	if !IsAuthError(wrapped) {
		t.Error("wrapped auth error should be detected via errors.As")
	}
}

func TestIsAuthError_PlainError(t *testing.T) {
	if IsAuthError(errors.New("some random error")) {
		t.Error("plain error should not be classified as auth")
	}
}

func TestIsAuthError_Nil(t *testing.T) {
	if IsAuthError(nil) {
		t.Error("nil error should not be classified as auth")
	}
}
