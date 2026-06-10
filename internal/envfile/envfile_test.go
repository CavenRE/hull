package envfile

import "testing"

func TestSetReplacesLive(t *testing.T) {
	in := "APP_NAME=Laravel\nDB_HOST=127.0.0.1\nDB_PORT=3306\n"
	got := Set(in, "DB_HOST", "db")
	want := "APP_NAME=Laravel\nDB_HOST=db\nDB_PORT=3306\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSetUncommentsWhenNoLiveLine(t *testing.T) {
	in := "APP_NAME=Laravel\n# DB_HOST=127.0.0.1\n"
	got := Set(in, "DB_HOST", "db")
	want := "APP_NAME=Laravel\nDB_HOST=db\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSetPrefersLiveOverCommented(t *testing.T) {
	in := "# DB_HOST=old\nDB_HOST=127.0.0.1\n"
	got := Set(in, "DB_HOST", "db")
	want := "# DB_HOST=old\nDB_HOST=db\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSetAppendsWhenMissing(t *testing.T) {
	got := Set("APP_NAME=x\n", "REDIS_HOST", "redis")
	want := "APP_NAME=x\nREDIS_HOST=redis\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSetAppendsWithoutTrailingNewline(t *testing.T) {
	got := Set("APP_NAME=x", "REDIS_HOST", "redis")
	want := "APP_NAME=x\nREDIS_HOST=redis\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSetEmptyContent(t *testing.T) {
	got := Set("", "KEY", "val")
	want := "KEY=val\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSetDoesNotMatchPrefixKeys(t *testing.T) {
	in := "DB_HOST_READONLY=a\nDB_HOST=b\n"
	got := Set(in, "DB_HOST", "db")
	want := "DB_HOST_READONLY=a\nDB_HOST=db\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSetValueWithSpecialChars(t *testing.T) {
	// v1's sed broke on '|' and '&' in values; the Go port must not.
	got := Set("KEY=old\n", "KEY", "a|b&c\\d")
	want := "KEY=a|b&c\\d\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSetPreservesCRLF(t *testing.T) {
	in := "A=1\r\nB=2\r\n"
	got := Set(in, "B", "3")
	want := "A=1\r\nB=3\r\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestGet(t *testing.T) {
	content := "# DB_HOST=commented\nDB_HOST=db\n"
	v, ok := Get(content, "DB_HOST")
	if !ok || v != "db" {
		t.Errorf("Get = %q, %v", v, ok)
	}
	if _, ok := Get(content, "MISSING"); ok {
		t.Error("Get(MISSING) should report not found")
	}
}
