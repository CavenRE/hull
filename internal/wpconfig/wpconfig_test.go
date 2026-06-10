package wpconfig

import (
	"strings"
	"testing"
)

const sample = `<?php
define( 'DB_NAME', 'wordpress' );
define( 'DB_USER', 'olduser' );
define('DB_HOST', 'localhost');
define( 'DB_PASSWORD', 'secret' );
$table_prefix = 'wp_';
`

func TestSetDefineSpacedStyle(t *testing.T) {
	got := SetDefine(sample, "DB_NAME", "myblog")
	if !strings.Contains(got, "define( 'DB_NAME', 'myblog' );") {
		t.Errorf("DB_NAME not replaced:\n%s", got)
	}
	if strings.Contains(got, "'wordpress'") {
		t.Error("old value still present")
	}
}

func TestSetDefineTightStyle(t *testing.T) {
	got := SetDefine(sample, "DB_HOST", "db")
	if !strings.Contains(got, "define( 'DB_HOST', 'db' );") {
		t.Errorf("DB_HOST not replaced:\n%s", got)
	}
}

func TestSetDefineEmptyValue(t *testing.T) {
	got := SetDefine(sample, "DB_PASSWORD", "")
	if !strings.Contains(got, "define( 'DB_PASSWORD', '' );") {
		t.Errorf("DB_PASSWORD not emptied:\n%s", got)
	}
}

func TestSetDefineEscapesQuotes(t *testing.T) {
	got := SetDefine(sample, "DB_PASSWORD", "it's")
	if !strings.Contains(got, `define( 'DB_PASSWORD', 'it\'s' );`) {
		t.Errorf("quote not escaped:\n%s", got)
	}
}

func TestSetDefineMissingIsNoop(t *testing.T) {
	if got := SetDefine(sample, "DB_CHARSET", "utf8"); got != sample {
		t.Error("missing define should leave content unchanged")
	}
}

func TestHasDefine(t *testing.T) {
	if !HasDefine(sample, "DB_NAME") {
		t.Error("HasDefine(DB_NAME) = false")
	}
	if HasDefine(sample, "DB_CHARSET") {
		t.Error("HasDefine(DB_CHARSET) = true")
	}
}

func TestEnsureProxyFixInserts(t *testing.T) {
	got := EnsureProxyFix(sample)
	if !strings.Contains(got, "HTTP_X_FORWARDED_PROTO") {
		t.Fatal("proxy fix not inserted")
	}
	if !strings.HasPrefix(got, "<?php\n\n// Hull reverse proxy fix") {
		t.Errorf("fix not directly after <?php:\n%.80s", got)
	}
	// All original content must survive.
	if !strings.Contains(got, "$table_prefix = 'wp_';") {
		t.Error("original content lost")
	}
}

func TestEnsureProxyFixIdempotent(t *testing.T) {
	once := EnsureProxyFix(sample)
	twice := EnsureProxyFix(once)
	if once != twice {
		t.Error("EnsureProxyFix is not idempotent")
	}
}
