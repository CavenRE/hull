package bundle

import "testing"

func TestDetectExtras(t *testing.T) {
	env := `MAIL_MAILER=smtp
MAIL_HOST=mailpit
SCOUT_DRIVER=meilisearch
MEILISEARCH_HOST="http://meilisearch:7700"
CACHE_STORE=memcached
FILESYSTEM_DISK=s3
AWS_ENDPOINT=http://minio:9000
`
	got := detectExtras(env)
	want := map[string]bool{"mailpit": true, "meilisearch": true, "memcached": true, "minio": true}
	if len(got) != len(want) {
		t.Fatalf("extras = %v, want %d distinct (%v)", got, len(want), want)
	}
	for _, e := range got {
		if !want[e] {
			t.Errorf("unexpected extra %q in %v", e, got)
		}
	}
}

func TestDetectExtrasConservative(t *testing.T) {
	// Stock Laravel (log mailer, no search/cache markers) → provision nothing.
	if got := detectExtras("MAIL_MAILER=log\nDB_CONNECTION=sqlite\n"); len(got) != 0 {
		t.Errorf("expected no extras, got %v", got)
	}
}
