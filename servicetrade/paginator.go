package servicetrade

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"net/url"
	"strconv"
	"strings"
)

// Paginate iterates over every item of a paginated collection endpoint. It
// requests pages sequentially, starting at page 1 and continuing until the
// totalPages reported by the API is reached. itemsKey names the field of each
// page holding the item list, for example "jobs" for /job. params are sent
// with every request.
//
// Each item is decoded into T. Iteration stops at the first error, which is
// yielded with a zero T; callers may also break out of the loop early.
//
//	for job, err := range servicetrade.Paginate[Job](ctx, client, "/job", "jobs", nil) {
//		if err != nil {
//			return err
//		}
//		use(job)
//	}
func Paginate[T any](
	ctx context.Context,
	c *Client,
	path, itemsKey string,
	params url.Values,
) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		var zero T
		totalPages := 1
		for page := 1; page <= totalPages; page++ {
			query := cloneValues(params)
			query.Set("page", strconv.Itoa(page))

			var raw json.RawMessage
			if _, err := c.Get(ctx, path, query, &raw); err != nil {
				yield(zero, err)
				return
			}
			if !isJSONObject(raw) {
				return
			}
			var body map[string]json.RawMessage
			if err := json.Unmarshal(raw, &body); err != nil {
				yield(zero, fmt.Errorf("servicetrade: decoding page %d: %w", page, err))
				return
			}
			totalPages = parseTotalPages(body["totalPages"])

			var items []T
			if rawItems, ok := body[itemsKey]; ok && isJSONArray(rawItems) {
				if err := json.Unmarshal(rawItems, &items); err != nil {
					yield(zero, fmt.Errorf("servicetrade: decoding %q on page %d: %w", itemsKey, page, err))
					return
				}
			}
			for _, item := range items {
				if !yield(item, nil) {
					return
				}
			}
		}
	}
}

func cloneValues(v url.Values) url.Values {
	out := make(url.Values, len(v)+1)
	for key, values := range v {
		out[key] = append([]string(nil), values...)
	}
	return out
}

func isJSONObject(raw json.RawMessage) bool { return firstByte(raw) == '{' }

func isJSONArray(raw json.RawMessage) bool { return firstByte(raw) == '[' }

func firstByte(raw json.RawMessage) byte {
	trimmed := bytes.TrimLeft(raw, " \t\r\n")
	if len(trimmed) == 0 {
		return 0
	}
	return trimmed[0]
}

// parseTotalPages mirrors the official SDKs' lenient handling of totalPages:
// numbers and numeric strings are accepted, anything else counts as one page.
func parseTotalPages(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 1
	}
	var n float64
	if err := json.Unmarshal(raw, &n); err == nil {
		return max(int(n), 1)
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
			return max(n, 1)
		}
	}
	return 1
}
