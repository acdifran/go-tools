package redact

import (
	"maps"
	"reflect"
	"testing"
)

func TestJSON(t *testing.T) {
	tests := []struct {
		name  string
		extra []string
		in    string
		want  string
	}{
		{
			name:  "top level key",
			extra: nil,
			in:    `{"password":"hunter2"}`,
			want:  `{"password":"[REDACTED]"}`,
		},
		{
			name:  "nested key",
			extra: nil,
			in:    `{"user":{"name":"ada","creds":{"apiKey":"sk-123"}}}`,
			want:  `{"user":{"name":"ada","creds":{"apiKey":"[REDACTED]"}}}`,
		},
		{
			name:  "key inside an array",
			extra: nil,
			in:    `{"accounts":[{"accessToken":"a"},{"accessToken":"b"}]}`,
			want:  `{"accounts":[{"accessToken":"[REDACTED]"},{"accessToken":"[REDACTED]"}]}`,
		},
		{
			name:  "escaped quote inside the value",
			extra: nil,
			in:    `{"password":"he said \"hi\"","keep":"me"}`,
			want:  `{"password":"[REDACTED]","keep":"me"}`,
		},
		{
			name:  "trailing backslash escape inside the value",
			extra: nil,
			in:    `{"apiKey":"a\\","keep":"me"}`,
			want:  `{"apiKey":"[REDACTED]","keep":"me"}`,
		},
		{
			name:  "several keys in one body",
			extra: nil,
			in:    `{"password":"p","apiToken":"t","apiKey":"k","clientSecret":"c","accessToken":"a","refreshToken":"r","keep":"me"}`,
			want:  `{"password":"[REDACTED]","apiToken":"[REDACTED]","apiKey":"[REDACTED]","clientSecret":"[REDACTED]","accessToken":"[REDACTED]","refreshToken":"[REDACTED]","keep":"me"}`,
		},
		{
			name:  "whitespace around the colon",
			extra: nil,
			in:    `{"password" :  "hunter2"}`,
			want:  `{"password":"[REDACTED]"}`,
		},
		{
			name:  "key name appearing as a value is untouched",
			extra: nil,
			in:    `{"note":"apiToken"}`,
			want:  `{"note":"apiToken"}`,
		},
		{
			name:  "similar key is untouched",
			extra: nil,
			in:    `{"clientSecretHint":"last 4 digits","xApiKey":"k","apikey":"k"}`,
			want:  `{"clientSecretHint":"last 4 digits","xApiKey":"k","apikey":"k"}`,
		},
		{
			name:  "different case is untouched",
			extra: nil,
			in:    `{"Password":"p","APIKEY":"k"}`,
			want:  `{"Password":"p","APIKEY":"k"}`,
		},
		{
			name:  "non string value is untouched",
			extra: nil,
			in:    `{"password":null,"apiKey":42}`,
			want:  `{"password":null,"apiKey":42}`,
		},
		{
			name:  "non json input unchanged",
			extra: nil,
			in:    "password: hunter2\nnot json at all",
			want:  "password: hunter2\nnot json at all",
		},
		{
			name:  "extra keys",
			extra: []string{"foo"},
			in:    `{"foo":"bar","password":"p","fooBar":"keep"}`,
			want:  `{"foo":"[REDACTED]","password":"[REDACTED]","fooBar":"keep"}`,
		},
		{
			name:  "empty input",
			extra: nil,
			in:    "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := []byte(tt.in)
			got := string(New(tt.extra...).JSON(in))
			if got != tt.want {
				t.Errorf("JSON(%s)\n got %s\nwant %s", tt.in, got, tt.want)
			}
			if string(in) != tt.in {
				t.Errorf("JSON mutated its input: %s", in)
			}
		})
	}
}

func TestJSONKeyWithRegexMetacharacters(t *testing.T) {
	r := New("a.c")
	got := string(r.JSON([]byte(`{"a.c":"secret","abc":"keep"}`)))
	want := `{"a.c":"[REDACTED]","abc":"keep"}`
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestValue(t *testing.T) {
	r := New("foo")

	in := map[string]any{
		"password": "hunter2",
		"foo":      "bar",
		"keep":     "me",
		"note":     "apiToken",
		"nested": map[string]any{
			"apiKey":           "sk-123",
			"clientSecretHint": "last 4",
			"deeper": []any{
				map[string]any{"accessToken": "a", "id": 1},
				"plain",
			},
		},
	}
	before := deepCopy(in)

	want := map[string]any{
		"password": Redacted,
		"foo":      Redacted,
		"keep":     "me",
		"note":     "apiToken",
		"nested": map[string]any{
			"apiKey":           Redacted,
			"clientSecretHint": "last 4",
			"deeper": []any{
				map[string]any{"accessToken": Redacted, "id": 1},
				"plain",
			},
		},
	}

	got := r.Value(in)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Value()\n got %#v\nwant %#v", got, want)
	}
	if !reflect.DeepEqual(in, before) {
		t.Errorf("Value mutated its input:\n got %#v\nwant %#v", in, before)
	}

	// The copy must not share the nested maps and slices with the input.
	gotNested, ok := got.(map[string]any)["nested"].(map[string]any)
	if !ok {
		t.Fatalf("nested is not a map[string]any: %#v", got.(map[string]any)["nested"])
	}
	gotNested["apiKey"] = "changed"
	if in["nested"].(map[string]any)["apiKey"] != "sk-123" {
		t.Error("Value returned a copy that shares nested maps with the input")
	}
}

func TestValueSliceAtTopLevel(t *testing.T) {
	r := New()
	in := []any{map[string]any{"password": "p"}, 7, nil}
	want := []any{map[string]any{"password": Redacted}, 7, nil}

	if got := r.Value(in); !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
	if in[0].(map[string]any)["password"] != "p" {
		t.Error("Value mutated its input")
	}
}

func TestValueScalarsPassThrough(t *testing.T) {
	r := New()
	for _, v := range []any{nil, "plain", 42, true} {
		if got := r.Value(v); !reflect.DeepEqual(got, v) {
			t.Errorf("Value(%#v) = %#v", v, got)
		}
	}
}

func TestNewDoesNotMutateDefaultKeys(t *testing.T) {
	before := append([]string(nil), DefaultKeys...)
	New("foo", "bar")
	if !reflect.DeepEqual(DefaultKeys, before) {
		t.Errorf("New mutated DefaultKeys: got %v, want %v", DefaultKeys, before)
	}
}

func deepCopy(m map[string]any) map[string]any {
	out := maps.Clone(m)
	for k, v := range out {
		switch t := v.(type) {
		case map[string]any:
			out[k] = deepCopy(t)
		case []any:
			s := append([]any(nil), t...)
			for i, e := range s {
				if em, ok := e.(map[string]any); ok {
					s[i] = deepCopy(em)
				}
			}
			out[k] = s
		}
	}
	return out
}
