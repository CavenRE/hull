// Package dbauth keeps the database logins Hull generates, one 0600 file per
// login under <hull home>/dbauth, so projects never share an account.
//
// A scope names where a login applies: "instance/<name>" for a shared
// instance (one login per project database, plus "_root"), and
// "project/<name>" for a project's dedicated database services. No stored
// login means a database Hull created before logins existed, which keeps its
// passwordless root.
package dbauth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/CavenRE/hull/internal/flock"
	"github.com/CavenRE/hull/internal/platform"
	"github.com/CavenRE/hull/internal/safefile"
)

// Login is a database account.
type Login struct {
	User     string `json:"user"`
	Password string `json:"password"`
}

func path(home, scope, name string) string {
	return filepath.Join(home, "dbauth", filepath.FromSlash(scope), name+".json")
}

// Get returns the stored login, ok=false when Hull never made one.
func Get(home, scope, name string) (Login, bool) {
	var l Login
	data, err := os.ReadFile(path(home, scope, name))
	if err != nil || json.Unmarshal(data, &l) != nil || l.Password == "" {
		return Login{}, false
	}
	return l, true
}

// Ensure returns the stored login, first creating one for user with a random
// password.
func Ensure(home, scope, name, user string) (Login, error) {
	unlock, err := flock.Home(home, "dbauth")
	if err != nil {
		return Login{}, err
	}
	defer unlock()
	if l, ok := Get(home, scope, name); ok {
		return l, nil
	}
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		return Login{}, err
	}
	l := Login{User: user, Password: base64.RawURLEncoding.EncodeToString(buf)}
	data, err := json.Marshal(l)
	if err != nil {
		return Login{}, err
	}
	p := path(home, scope, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return Login{}, err
	}
	if err := safefile.Write(p, data, 0o600); err != nil {
		return Login{}, err
	}
	return l, platform.RestrictToUser(p)
}

// Names lists the logins stored in a scope, sorted.
func Names(home, scope string) []string {
	entries, err := os.ReadDir(filepath.Join(home, "dbauth", filepath.FromSlash(scope)))
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if n, ok := strings.CutSuffix(e.Name(), ".json"); ok && !e.IsDir() {
			names = append(names, n)
		}
	}
	return names
}

// Forget removes every login in a scope (the instance or project is gone).
func Forget(home, scope string) error {
	err := os.RemoveAll(filepath.Join(home, "dbauth", filepath.FromSlash(scope)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
