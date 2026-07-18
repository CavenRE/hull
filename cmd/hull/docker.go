package main

import (
	"context"
	"fmt"

	"github.com/CavenRE/hull/internal/dockerx"
)

// ensureDocker makes sure the container engine is up before a command needs it,
// starting it when it is not. Hull used to just fail with "the container engine
// is not responding" when the fix was simply to launch Docker, which meant
// `hull up` looked broken on a machine where Docker was merely closed.
//
// Progress is printed because a cold Docker Desktop start takes real time and a
// silent pause reads as a hang.
func ensureDocker(ctx context.Context) error {
	return dockerx.EnsureEngine(ctx, func(msg string) { fmt.Println(msg) })
}
