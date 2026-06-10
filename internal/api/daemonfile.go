package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	return os.WriteFile(filepath.Join(hullHome, DaemonFileName), data, 0o600)
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
