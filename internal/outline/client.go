// Package outline is a client for the Outline REST API: every call is a
// POST to {base}/api/<method> with bearer auth, responses wrapped in
// {"ok":true,"data":...}.
package outline

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Client posts JSON to the Outline API and unwraps the envelope.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// APIError is a structured failure returned by the Outline API.
type APIError struct {
	Status int    // HTTP status code
	Method string // Outline method, e.g. "documents.search"
	Detail string // API error string from the response body
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s: HTTP %d: %s", e.Method, e.Status, e.Detail)
}

type envelope struct {
	OK         bool            `json:"ok"`
	Data       json.RawMessage `json:"data"`
	Pagination json.RawMessage `json:"pagination"`
	Error      string          `json:"error"`
	Message    string          `json:"message"`
	Success    *bool           `json:"success"`
}

// Do posts body to {base}/api/<method> and returns the unwrapped "data".
func (c *Client) Do(ctx context.Context, method string, body any) (json.RawMessage, error) {
	env, err := c.post(ctx, method, body)
	if err != nil {
		return nil, err
	}
	if len(env.Data) == 0 {
		if env.Success != nil {
			return json.Marshal(map[string]bool{"success": *env.Success})
		}
		return json.RawMessage("null"), nil
	}
	return env.Data, nil
}

const maxPages = 50

// Paged retains request filters across pages and fails rather than returning
// incomplete results when the request safety limit is reached.
func (c *Client) Paged(ctx context.Context, method string, body map[string]any) (json.RawMessage, error) {
	body = maps.Clone(body)
	if body == nil {
		body = map[string]any{}
	}
	offset, err := initialOffset(method, body)
	if err != nil {
		return nil, err
	}
	items := []json.RawMessage{}
	for range maxPages {
		env, err := c.post(ctx, method, body)
		if err != nil {
			return nil, err
		}
		more, p, err := decodePage(method, env)
		if err != nil {
			return nil, err
		}
		items = append(items, more...)
		if pageComplete(offset, more, p) {
			return json.Marshal(items)
		}
		offset, err = nextOffset(method, p.NextPath, offset, body)
		if err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("%s: pagination reached the %d-page safety limit before completion; narrow the query or use bounded --limit/--offset requests", method, maxPages)
}

// pagination carries the continuation metadata surfaced per page.
type pagination struct {
	NextPath string `json:"nextPath"`
	Limit    int    `json:"limit"`
	Total    *int   `json:"total"`
}

// initialOffset validates the requested starting offset, defaulting to zero.
func initialOffset(method string, body map[string]any) (int, error) {
	value, ok := body["offset"]
	if !ok {
		return 0, nil
	}
	offset, err := strconv.Atoi(fmt.Sprint(value))
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("%s: invalid pagination offset", method)
	}
	return offset, nil
}

// decodePage reads both the result array and its continuation metadata.
func decodePage(method string, env *envelope) ([]json.RawMessage, pagination, error) {
	var page []json.RawMessage
	var p pagination
	if len(env.Data) == 0 || env.Data[0] != '[' {
		return nil, p, fmt.Errorf("%s: expected array for pagination", method)
	}
	if err := json.Unmarshal(env.Data, &page); err != nil {
		return nil, p, fmt.Errorf("%s: decoding page: %w", method, err)
	}
	if len(env.Pagination) > 0 {
		if err := json.Unmarshal(env.Pagination, &p); err != nil {
			return nil, p, fmt.Errorf("%s: invalid pagination: %w", method, err)
		}
	}
	return page, p, nil
}

// pageComplete reports whether the page ended the result set before the next page.
func pageComplete(offset int, page []json.RawMessage, p pagination) bool {
	return len(page) == 0 || p.NextPath == "" ||
		(p.Total != nil && offset+len(page) >= *p.Total) ||
		(p.Total == nil && p.Limit > 0 && len(page) < p.Limit)
}

// nextOffset validates the continuation path, advances the request offset, and
// carries over an explicit page limit from the server.
func nextOffset(method, nextPath string, offset int, body map[string]any) (int, error) {
	next, err := url.Parse(nextPath)
	if err != nil || next.IsAbs() || next.Host != "" || next.Fragment != "" || next.Path != "/api/"+method {
		return 0, fmt.Errorf("%s: invalid pagination endpoint", method)
	}
	query, err := url.ParseQuery(next.RawQuery)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid pagination query: %w", method, err)
	}
	nextOffset, err := strconv.Atoi(query.Get("offset"))
	if err != nil || nextOffset <= offset {
		return 0, fmt.Errorf("%s: pagination offset did not advance", method)
	}
	body["offset"] = nextOffset
	if value := query.Get("limit"); value != "" {
		limit, err := strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 100 {
			return 0, fmt.Errorf("%s: invalid pagination limit", method)
		}
		body["limit"] = limit
	}
	return nextOffset, nil
}

// maxAttempts bounds requests per call: one original + one 429 retry.
const maxAttempts = 2

// maxRetryDelay caps the Retry-After wait so agents never hang for long.
const maxRetryDelay = 10 * time.Second

var methodPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*\.[A-Za-z][A-Za-z0-9_]*$`)

// post performs one API call, retrying once on 429 per the Retry-After
// header, and returns the full envelope.
func (c *Client) post(ctx context.Context, method string, body any) (*envelope, error) {
	if !methodPattern.MatchString(method) {
		return nil, fmt.Errorf("invalid API method %q: expected domain.action", method)
	}
	if body == nil {
		body = map[string]any{}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("%s: encoding request: %w", method, err)
	}
	for attempt := range maxAttempts {
		raw, resp, err := c.doRequest(ctx, method, payload)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusTooManyRequests && attempt < maxAttempts-1 {
			if err := retryWait(ctx, method, resp.Header); err != nil {
				return nil, err
			}
			continue
		}
		return decodeEnvelope(method, raw, resp.StatusCode)
	}
	return nil, fmt.Errorf("%s: exhausted retries", method)
}

// doRequest sends one authenticated POST and closes the response after reading it.
func (c *Client) doRequest(ctx context.Context, method string, payload []byte) ([]byte, *http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/api/"+method, bytes.NewReader(payload))
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", method, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", method, err)
	}
	raw, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, nil, fmt.Errorf("%s: reading response: %w", method, err)
	}
	return raw, resp, nil
}

// decodeEnvelope parses the response body and rejects non-success envelopes.
func decodeEnvelope(method string, raw []byte, status int) (*envelope, error) {
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("%s: HTTP %d: decoding response: %w", method, status, err)
	}
	if status < 200 || status >= 300 || !env.OK || (env.Success != nil && !*env.Success) {
		return nil, &APIError{Status: status, Method: method, Detail: cmp.Or(env.Message, env.Error, http.StatusText(status))}
	}
	return &env, nil
}

// retryWait sleeps per Retry-After until the context is canceled.
func retryWait(ctx context.Context, method string, header http.Header) error {
	wait := retryDelay(header.Get("Retry-After"))
	select {
	case <-ctx.Done():
		return fmt.Errorf("%s: %w", method, ctx.Err())
	case <-time.After(wait):
		return nil
	}
}

// retryDelay accepts delta seconds or HTTP dates without overflowing durations.
func retryDelay(v string) time.Duration {
	v = strings.TrimSpace(v)
	if secs, err := strconv.ParseInt(v, 10, 64); err == nil {
		if secs < 0 {
			return time.Second
		}
		return time.Duration(min(secs, int64(maxRetryDelay/time.Second))) * time.Second
	}
	if date, err := http.ParseTime(v); err == nil {
		return max(0, min(time.Until(date), maxRetryDelay))
	}
	return time.Second
}
