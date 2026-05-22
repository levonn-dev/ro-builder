package scoring

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// maxScoreResponseBytes caps the sidecar response body the client will
// buffer. A healthy /score reply is well under 100 KB; the cap exists
// to bound memory if a misbehaving (or compromised) sidecar streams an
// unbounded body.
const maxScoreResponseBytes = 10 * 1024 * 1024 // 10 MB

// Client drives a running calc-sidecar over its POST /score HTTP endpoint.
//
// The sidecar processes requests sequentially; Node's event loop runs one
// task to completion before picking up the next, and score() never yields;
// so concurrent in-flight requests serialize at the sidecar. A single Client
// is safe to share across goroutines; just don't expect parallelism.
type Client struct {
	baseURL string
	httpC   *http.Client
}

// NewClient constructs a sidecar client. baseURL is the sidecar's root URL
// (e.g. "http://localhost:7401"). httpC may be nil to use a default client
// with a bounded timeout; never http.DefaultClient, whose zero timeout
// would let a crashed-but-socket-alive sidecar hang the caller forever
// when the caller's context has no deadline.
func NewClient(baseURL string, httpC *http.Client) *Client {
	if httpC == nil {
		httpC = &http.Client{Timeout: 60 * time.Second}
	}
	return &Client{baseURL: baseURL, httpC: httpC}
}

// Error is returned when the sidecar reports a non-2xx response.
//
// Status is the HTTP status code and Message is the sidecar's "error" field.
// 4xx errors signal client-fixable input (unknown slot, refine on a
// non-refinable slot, unmapped iRO id, item not in current class's option
// list); 5xx errors signal a sidecar/shim crash and should be
// treated as infrastructure failures (retry or surface, don't reshape the
// request and try again).
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("calc-sidecar HTTP %d: %s", e.Status, e.Message)
}

// IsClient reports whether the error is a 4xx (caller's input was wrong)
// rather than a 5xx (sidecar bug or transient failure). Callers extract via
// errors.As(err, &scoring.Error{}).
func (e *Error) IsClient() bool {
	return e.Status >= 400 && e.Status < 500
}

// IsClientError is the errors.As-friendly way to ask the question without
// declaring an *Error variable at the call site.
func IsClientError(err error) bool {
	var sErr *Error
	return errors.As(err, &sErr) && sErr.IsClient()
}

// Score posts req to the sidecar and decodes the response. Returns *Error
// (errors.As-extractable) on non-2xx HTTP responses; other errors (transport,
// JSON decode) come back as plain wrapped errors.
//
// A nil req is rejected; callers must construct an explicit ScoreRequest,
// even an empty one, so a nil pointer at the call site doesn't silently
// score the shim's reset baseline (Novice / lvl 1).
func (c *Client) Score(ctx context.Context, req *ScoreRequest) (*ScoreResponse, error) {
	if req == nil {
		return nil, errors.New("scoring: req is required")
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/score", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")

	resp, err := c.httpC.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call sidecar: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxScoreResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// The sidecar returns { "error": "..." } on failure but be tolerant
		// of malformed bodies; a transport-level non-200 with garbage JSON
		// shouldn't mask the status code from the caller.
		var errBody struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(respBody, &errBody)
		msg := errBody.Error
		if msg == "" {
			msg = string(respBody)
		}
		return nil, &Error{Status: resp.StatusCode, Message: msg}
	}

	var out ScoreResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &out, nil
}
