package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/CavenRE/hull/internal/platform"
	"github.com/CavenRE/hull/internal/safefile"
)

// DaemonFileName is written into the Hull home directory by a running
// daemon so clients can find and authenticate to it.
const DaemonFileName = "daemon.json"

// DaemonInfo is the discovery record for a running daemon. The transport is
// HTTP on localhost guarded by a bearer token in a 0600 file (ADR 0006).
type DaemonInfo struct {
	Port  int    `json:"port"`
	Token string `json:"token"`
	PID   int    `json:"pid"`
	// Hello is set by a daemon that answers /v1/hello, so a client never takes
	// an older daemon's reply from whatever now holds this file's port.
	Hello bool `json:"hello,omitempty"`
}

// NewToken returns a fresh random bearer token.
func NewToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// WriteDaemonFile records a running daemon in the Hull home directory.
func WriteDaemonFile(hullHome string, info DaemonInfo) error {
	if err := os.MkdirAll(hullHome, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}
	path := filepath.Join(hullHome, DaemonFileName)
	if err := safefile.Write(path, data, 0o600); err != nil {
		return err
	}
	return platform.RestrictToUser(path) // the token is a key to the whole API
}

// ReadDaemonFile loads the discovery record, if present.
func ReadDaemonFile(hullHome string) (*DaemonInfo, error) {
	data, err := os.ReadFile(filepath.Join(hullHome, DaemonFileName))
	if err != nil {
		return nil, err
	}
	var info DaemonInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("corrupt %s: %w", DaemonFileName, err)
	}
	return &info, nil
}

// RemoveDaemonFile deletes the discovery record (daemon shutdown).
func RemoveDaemonFile(hullHome string) {
	_ = os.Remove(filepath.Join(hullHome, DaemonFileName))
}
