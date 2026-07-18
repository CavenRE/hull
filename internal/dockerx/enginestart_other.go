//go:build !windows && !darwin && !linux

package dockerx

import (
	"context"
	"errors"
)

// startEngine is unsupported off the three shipped platforms.
func startEngine(ctx context.Context) (string, error) {
	return "", errors.New("Hull cannot start the container engine on this platform; start it manually")
}
