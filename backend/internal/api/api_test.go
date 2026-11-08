package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/godvlpr/hookymaster/internal/app"
)

// fakeService returns err from every call it implements.
type fakeService struct {
	Service
	err      error
	selected struct {
		id   int64
		text string
	}
}

func (f *fakeService) Project(context.Context, string) (app.ProjectView, error) {
	return app.ProjectView{Code: "O12"}, f.err
}

func (f *fakeService) SelectCandidate(_ context.Context, id int64, in app.HookInput) error {
	f.selected.id, f.selected.text = id, in.Text
	return f.err
}

func (f *fakeService) Export(context.Context, []int64) ([]byte, error) {
	return []byte("ad_name\n"), f.err
}

func do(t *testing.T, svc Service, method, path, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := New(svc, zerolog.Nop(), "").Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func TestErrorMapping(t *testing.T) {
	tests := []struct {
		err  error
		want int
	}{
		{nil, http.StatusOK},
		{fmt.Errorf("%w: project O99", app.ErrNotFound), http.StatusNotFound},
		{fmt.Errorf("%w: bad", app.ErrInvalid), http.StatusBadRequest},
		{fmt.Errorf("%w: taken", app.ErrConflict), http.StatusConflict},
		{app.ErrNoModel, http.StatusServiceUnavailable},
		{errors.New("db is down"), http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.err), func(t *testing.T) {
			status, body := do(t, &fakeService{err: tt.err}, http.MethodGet, "/api/projects/O12", "")
			if status != tt.want {
				t.Errorf("status: got %d, want %d (%s)", status, tt.want, body)
			}
			if tt.want == http.StatusInternalServerError && strings.Contains(body, "db is down") {
				t.Error("internal error details must not leak")
			}
			if strings.Contains(body, "invalid input") || strings.Contains(body, "not found:") {
				t.Errorf("message must be readable: %s", body)
			}
		})
	}
}

func TestSelectCandidate(t *testing.T) {
	svc := &fakeService{}
	status, _ := do(t, svc, http.MethodPost, "/api/candidates/7/select", `{"text":"New hook","onScreenText":"x"}`)
	if status != http.StatusNoContent || svc.selected.id != 7 || svc.selected.text != "New hook" {
		t.Errorf("got %d %+v", status, svc.selected)
	}

	status, _ = do(t, svc, http.MethodPost, "/api/candidates/abc/select", `{}`)
	if status != http.StatusBadRequest {
		t.Errorf("bad id: got %d", status)
	}
}

func TestExportIsCSV(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/queue/export", strings.NewReader(`{"ids":[1]}`))
	req.Header.Set("Content-Type", "application/json")
	res, err := New(&fakeService{}, zerolog.Nop(), "").Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("content type: %s", ct)
	}
}

func TestUnknownAPIRoute(t *testing.T) {
	status, body := do(t, &fakeService{}, http.MethodGet, "/api/nope", "")
	var e errorBody
	if status != http.StatusNotFound || json.Unmarshal([]byte(body), &e) != nil {
		t.Errorf("got %d %s", status, body)
	}
}

func TestIndexIsNotCached(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<div id=root></div>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "index-abc.js"), []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := New(&fakeService{}, zerolog.Nop(), dir)
	for _, tc := range []struct {
		path, want string
	}{
		{"/", "no-cache"},
		{"/projects/O12/import", "no-cache"}, // client-side route
		{"/assets/index-abc.js", ""},
	} {
		resp, err := f.Test(httptest.NewRequest(http.MethodGet, tc.path, nil))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Cache-Control") != tc.want {
			t.Errorf("%s: status %d, Cache-Control %q, want %q", tc.path, resp.StatusCode, resp.Header.Get("Cache-Control"), tc.want)
		}
	}
}
