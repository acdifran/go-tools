package servicetrade

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"testing"
)

type pagedJob struct {
	ID int `json:"id"`
}

func TestPaginate_WalksAllPages(t *testing.T) {
	srv, mux := newServer(t)
	rec := &recorder{}
	pages := map[string][]map[string]any{
		"1": {{"id": 1}, {"id": 2}},
		"2": {{"id": 3}},
		"3": {{"id": 4}},
	}
	mux.HandleFunc("GET /api/job", func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
			"totalPages": 3,
			"jobs":       pages[r.URL.Query().Get("page")],
		}})
	})
	c := newTestClient(t, srv, WithToken(validToken()))

	var ids []int
	for job, err := range Paginate[pagedJob](context.Background(), c, "/job", "jobs", url.Values{"status": {"open"}}) {
		if err != nil {
			t.Fatalf("Paginate: %v", err)
		}
		ids = append(ids, job.ID)
	}
	if !slices.Equal(ids, []int{1, 2, 3, 4}) {
		t.Errorf("ids = %v, want [1 2 3 4]", ids)
	}
	if rec.count() != 3 {
		t.Fatalf("requested %d pages, want 3", rec.count())
	}
	for i := range 3 {
		q := rec.get(i).URL.Query()
		if got := q.Get("page"); got != strconv.Itoa(i+1) {
			t.Errorf("request %d page = %q, want %d", i, got, i+1)
		}
		if got := q.Get("status"); got != "open" {
			t.Errorf("request %d dropped the status param, got %q", i, got)
		}
	}
}

func TestPaginate_LenientTotalPages(t *testing.T) {
	srv, mux := newServer(t)
	for path, totalPages := range map[string]any{
		"/api/numeric-string": "2",
		"/api/missing":        nil,
		"/api/garbage":        "abc",
		"/api/zero":           0,
		"/api/float":          2.7,
	} {
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			data := map[string]any{"items": []map[string]any{{"id": page}}}
			if totalPages != nil {
				data["totalPages"] = totalPages
			}
			writeJSON(w, http.StatusOK, map[string]any{"data": data})
		})
	}
	c := newTestClient(t, srv, WithToken(validToken()))

	for path, want := range map[string]int{
		"/numeric-string": 2,
		"/missing":        1,
		"/garbage":        1,
		"/zero":           1,
		"/float":          2,
	} {
		var count int
		for _, err := range Paginate[pagedJob](context.Background(), c, path, "items", nil) {
			if err != nil {
				t.Fatalf("Paginate %s: %v", path, err)
			}
			count++
		}
		if count != want {
			t.Errorf("%s yielded %d items, want %d", path, count, want)
		}
	}
}

func TestPaginate_MissingItemsKeyAndNonObjectBody(t *testing.T) {
	srv, mux := newServer(t)
	mux.HandleFunc("GET /api/noitems", dataResponse(map[string]any{"totalPages": 1}))
	mux.HandleFunc("GET /api/notalist", dataResponse(map[string]any{"totalPages": 1, "items": "nope"}))
	mux.HandleFunc("GET /api/list", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, []int{1, 2, 3})
	})
	c := newTestClient(t, srv, WithToken(validToken()))

	for _, path := range []string{"/noitems", "/notalist", "/list"} {
		for item, err := range Paginate[map[string]any](context.Background(), c, path, "items", nil) {
			t.Errorf("%s yielded item=%v err=%v, want nothing", path, item, err)
		}
	}
}

func TestPaginate_PropagatesErrors(t *testing.T) {
	srv, mux := newServer(t)
	mux.HandleFunc("GET /api/bad", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"messages": map[string]any{"error": []string{"nope"}}})
	})
	c := newTestClient(t, srv, WithToken(validToken()))

	var errs []error
	for _, err := range Paginate[pagedJob](context.Background(), c, "/bad", "items", nil) {
		errs = append(errs, err)
	}
	if len(errs) != 1 {
		t.Fatalf("yielded %d results, want exactly one error", len(errs))
	}
	var apiErr *APIError
	if !errors.As(errs[0], &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("error = %v, want *APIError 400", errs[0])
	}
}

func TestPaginate_StopsWhenCallerBreaks(t *testing.T) {
	srv, mux := newServer(t)
	rec := &recorder{}
	mux.HandleFunc("GET /api/job", func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
			"totalPages": 5,
			"jobs":       []map[string]any{{"id": page}},
		}})
	})
	c := newTestClient(t, srv, WithToken(validToken()))

	for job, err := range Paginate[pagedJob](context.Background(), c, "/job", "jobs", nil) {
		if err != nil {
			t.Fatalf("Paginate: %v", err)
		}
		if job.ID == 1 {
			break
		}
	}
	if rec.count() != 1 {
		t.Errorf("requested %d pages after break, want 1", rec.count())
	}
}
