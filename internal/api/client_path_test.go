package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientEscapesPathNames(t *testing.T) {
	var got string
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /v1/projects/{name}", func(w http.ResponseWriter, r *http.Request) {
		got = r.PathValue("name")
		writeJSON(w, http.StatusAccepted, JobRef{})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	for _, name := range []string{"my shop", "a/b", "x?y#z"} {
		got = ""
		if _, err := c.DeleteProject(context.Background(), name); err != nil {
			t.Fatalf("%q: %v", name, err)
		}
		if got != name {
			t.Errorf("server saw %q, want %q", got, name)
		}
	}
}
