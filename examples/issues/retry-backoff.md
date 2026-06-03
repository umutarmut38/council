# Example Issue: Retry HTTP Requests With Backoff

Add retry-with-backoff behavior to the HTTP client.

Requirements:

- Retry only transient failures: network errors, HTTP 408, HTTP 429, and HTTP
  5xx responses.
- Do not retry non-idempotent requests unless the caller opts in.
- Use exponential backoff with jitter.
- Respect request context cancellation and deadlines.
- Add focused tests for success after retry, exhausted retries, cancellation,
  and non-retryable status codes.

Run it through council:

```text
/plan @examples/issues/retry-backoff.md
/vote
/build
/start-build
/review
/adopt
```
