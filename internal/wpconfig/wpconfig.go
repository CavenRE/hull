// Package wpconfig patches WordPress wp-config.php files, porting the sed
// edits from v1's import command (define() rewrites and the reverse-proxy
// HTTPS fix).
package wpconfig

import (
	"regexp"
	"strings"
)

var phpEscaper = strings.NewReplacer(`\`, `\\`, `'`, `\'`)

// SetDefine replaces the value of define('NAME', '...'), tolerating the
// spacing variants WordPress ships ("define( 'X', 'y' );" etc.). Content is
// returned unchanged when the define is absent.
func SetDefine(content, name, value string) string {
	re := regexp.MustCompile(`define\(\s*['"]` + regexp.QuoteMeta(name) + `['"]\s*,\s*['"][^'"]*['"]\s*\)`)
	return re.ReplaceAllString(content, "define( '"+name+"', '"+phpEscaper.Replace(value)+"' )")
}

// HasDefine reports whether a define('NAME', ...) statement exists.
func HasDefine(content, name string) bool {
	re := regexp.MustCompile(`define\(\s*['"]` + regexp.QuoteMeta(name) + `['"]`)
	return re.MatchString(content)
}

const proxyFix = `// Hull reverse proxy fix
if (isset($_SERVER['HTTP_X_FORWARDED_PROTO']) && strpos($_SERVER['HTTP_X_FORWARDED_PROTO'], 'https') !== false) {
    $_SERVER['HTTPS'] = 'on';
}
`

// EnsureProxyFix inserts the X-Forwarded-Proto HTTPS fix after the opening
// <?php tag unless the file already references the header. WordPress behind
// Hull's terminating proxy needs this to avoid redirect loops.
func EnsureProxyFix(content string) string {
	if strings.Contains(content, "HTTP_X_FORWARDED_PROTO") {
		return content
	}
	idx := strings.Index(content, "<?php")
	if idx < 0 {
		return content
	}
	after := idx + len("<?php")
	return content[:after] + "\n\n" + proxyFix + content[after:]
}
