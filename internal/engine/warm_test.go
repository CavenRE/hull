package engine

import (
	"net/http"
	"testing"
)

func TestLocalRedirectsStayLocal(t *testing.T) {
	check := LocalRedirects(2, "test")
	first, _ := http.NewRequest(http.MethodGet, "https://shop.test/", nil)
	for _, c := range []struct {
		to   string
		hops int
		want error
	}{
		{"https://shop.test/login", 1, nil},
		{"https://admin.shop.test/", 1, nil},
		{"https://evil.com/", 1, http.ErrUseLastResponse},
		{"https://shop.test/again", 2, http.ErrUseLastResponse},
	} {
		req, _ := http.NewRequest(http.MethodGet, c.to, nil)
		via := make([]*http.Request, c.hops)
		for i := range via {
			via[i] = first
		}
		if got := check(req, via); got != c.want {
			t.Errorf("redirect to %s after %d hop(s) = %v, want %v", c.to, c.hops, got, c.want)
		}
	}
}
