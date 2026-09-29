// Package redact hides secret values in data that is about to be logged.
package redact

import (
	"regexp"
	"slices"
	"strings"
)

// Redacted is the placeholder written in place of every secret value.
const Redacted = "[REDACTED]"

// DefaultKeys are the keys whose values every Redactor hides.
var DefaultKeys = []string{
	"password",
	"apiToken",
	"apiKey",
	"clientSecret",
	"accessToken",
	"refreshToken",
}

// Redactor replaces the values of a fixed set of keys.
//
// Keys match exactly and case-sensitively: "apiKey" matches "apiKey" and
// nothing else, so "apikey", "myApiKey" and "apiKeyHint" are all left alone.
//
// A Redactor is immutable after New and safe for concurrent use.
type Redactor struct {
	keys map[string]struct{}
	re   *regexp.Regexp
}

// New returns a Redactor for DefaultKeys plus extraKeys.
func New(extraKeys ...string) *Redactor {
	keys := make(map[string]struct{}, len(DefaultKeys)+len(extraKeys))
	patterns := make([]string, 0, len(DefaultKeys)+len(extraKeys))

	for _, k := range slices.Concat(DefaultKeys, extraKeys) {
		if _, seen := keys[k]; k == "" || seen {
			continue
		}
		keys[k] = struct{}{}
		patterns = append(patterns, regexp.QuoteMeta(k))
	}

	var re *regexp.Regexp
	if len(patterns) > 0 {
		// A JSON string value: any run of non-quote, non-backslash bytes and
		// backslash escapes, so an escaped quote inside the value does not end it.
		re = regexp.MustCompile(`(?s)"(` + strings.Join(patterns, "|") + `)"\s*:\s*"(?:[^"\\]|\\.)*"`)
	}

	return &Redactor{keys: keys, re: re}
}

// JSON replaces the string value of every configured key with Redacted, at any
// nesting depth, by rewriting the raw text rather than parsing and
// re-serializing it. Input that is not JSON, or that holds none of the keys, is
// returned unchanged. The input slice is never modified.
func (r *Redactor) JSON(b []byte) []byte {
	if r == nil || r.re == nil || len(b) == 0 {
		return b
	}
	return r.re.ReplaceAll(b, []byte(`"${1}":"`+Redacted+`"`))
}

// Value returns a deep copy of an already-decoded value, such as GraphQL
// variables, with the value of every configured key replaced by Redacted. It
// walks map[string]any and []any -- the shapes JSON decodes into -- and returns
// anything else as is. The input is never mutated.
func (r *Redactor) Value(v any) any {
	if r == nil {
		return v
	}

	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if _, ok := r.keys[k]; ok {
				out[k] = Redacted
				continue
			}
			out[k] = r.Value(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = r.Value(val)
		}
		return out
	default:
		return v
	}
}
