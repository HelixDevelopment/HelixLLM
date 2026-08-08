package health

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// httpProbeBodyLimit caps how much of a failing response body is read back
// into the health report's message. Enough to identify the responder, small
// enough that a misconfigured URL pointing at a large document cannot turn a
// health poll into a bandwidth event.
const httpProbeBodyLimit = 256

// HTTPGetCheck returns a CheckFunc that performs a REAL HTTP GET against url
// and reports the dependency unhealthy unless it answers with a 2xx status.
//
// The request is built with http.NewRequestWithContext, so the aggregator's
// per-component deadline genuinely cancels an in-flight request rather than
// merely abandoning it. The response body is drained (bounded) and closed so
// the connection returns to the pool instead of leaking one socket per poll.
//
// Anti-bluff note: a non-2xx status is a FAILURE, not a success. Treating "the
// server answered something" as healthy is the absence-of-error verdict — the
// dependency answering 500 on every request would report green forever.
//
// client must be non-nil and SHOULD carry its own Timeout as a backstop for
// callers that hand in a context without a deadline.
func HTTPGetCheck(client *http.Client, url string) CheckFunc {
	return func(ctx context.Context) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("http probe %s: build request: %w", url, err)
		}

		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("http probe %s: %w", url, err)
		}
		defer resp.Body.Close() //nolint:errcheck

		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, httpProbeBodyLimit))
			return fmt.Errorf("http probe %s: status %d: %s",
				url, resp.StatusCode, string(body))
		}

		// Drain so the connection is reusable by the next poll.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, httpProbeBodyLimit))
		return nil
	}
}
