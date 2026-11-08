package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New("test-key", "chat-model", "embed-model")
	c.baseURL = srv.URL
	return c
}

func TestJSON(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "chat-model" {
			http.Error(w, "bad model", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"ok\":true}"},"finish_reason":"stop"}]}`))
	})
	got, err := c.JSON(context.Background(), "sys", "user")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"ok":true}` {
		t.Errorf("got %s", got)
	}
}

func TestJSONErrors(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{"api error", http.StatusUnauthorized, `{"error":{"message":"invalid key"}}`, "status 401"},
		{"cut off", http.StatusOK, `{"choices":[{"message":{"content":"{"},"finish_reason":"length"}]}`, "finish reason"},
		{"no choices", http.StatusOK, `{"choices":[]}`, "no choices"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			})
			_, err := c.JSON(context.Background(), "s", "u")
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("got %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestEmbedKeepsInputOrder(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"index":1,"embedding":[2]},{"index":0,"embedding":[1]}]}`))
	})
	got, err := c.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if got[0][0] != 1 || got[1][0] != 2 {
		t.Errorf("got %v", got)
	}
}
