package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPublishedHostPorts(t *testing.T) {
	dir := t.TempDir()
	yaml := `services:
  a:
    ports:
      - "127.0.0.2:8081:8081"
      - "5432:5432"
      - "80"
      - "9090/tcp"
  b:
    ports:
      - target: 9000
        published: 9000
        host_ip: 127.0.0.1
`
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	got := map[int]string{}
	for _, hp := range publishedHostPorts(dir, nil) {
		got[hp.port] = hp.host
	}
	if got[8081] != "127.0.0.2" {
		t.Errorf("8081 host = %q, want 127.0.0.2", got[8081])
	}
	if _, ok := got[5432]; !ok {
		t.Error("missing host port 5432 (host:container short form)")
	}
	if got[9000] != "127.0.0.1" {
		t.Errorf("9000 host = %q, want 127.0.0.1 (long form)", got[9000])
	}
	// "80" and "9090/tcp" are container-only , no fixed host port to clash.
	if _, ok := got[80]; ok {
		t.Error("container-only port 80 should not be treated as a published host port")
	}
	if _, ok := got[9090]; ok {
		t.Error("container-only port 9090 should not be treated as a published host port")
	}
}
