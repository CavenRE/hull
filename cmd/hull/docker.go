package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/dockerx"
)

// Engine requirement, declared per command and enforced in one place.
//
// This replaces scattered ensureDocker calls inside command bodies. That
// approach failed twice over: six commands (status, exec, artisan, npm, stop,
// forget) never had a guard to begin with, and a dozen more put theirs inside
// the in-process closure of withDaemon, so the daemon-routed path (the normal
// case) ran completely unguarded. An annotation cannot be forgotten, because
// engineAnnotationTest fails the build when a command omits it.
const engineAnnotation = "hull:engine"

const (
	// engineEnsure: the command acts on containers, so start the engine if it is
	// merely closed and wait for it.
	engineEnsure = "ensure"
	// engineCheck: the command only reports, so fail fast with a clean message.
	// Never launch Docker Desktop for a read: it is slow, surprising, and for a
	// diagnostic it destroys the thing being diagnosed.
	engineCheck = "check"
	// engineNone: the command never touches the engine.
	engineNone = "none"
)

// engineModes declares what every command needs from the container engine, in
// one place rather than scattered through 25 command files. Anything absent
// defaults to engineNone and is reported by TestEveryCommandDeclaresEngineNeed,
// so a new command cannot quietly ship without a decision being made.
var engineModes = map[string]string{
	// Acts on containers: start the engine if it is merely closed.
	"hull up":             engineEnsure,
	"hull down":           engineEnsure,
	"hull restart":        engineEnsure,
	"hull repair":         engineEnsure,
	"hull rebuild":        engineEnsure,
	"hull reset":          engineEnsure,
	"hull rm":             engineEnsure,
	"hull new":            engineEnsure,
	"hull import":         engineEnsure,
	"hull link":           engineEnsure,
	"hull unlink":         engineEnsure,
	"hull migrate":        engineEnsure,
	"hull exec":           engineEnsure,
	"hull artisan":        engineEnsure,
	"hull npm":            engineEnsure,
	"hull export":         engineEnsure, // takes live database dumps
	"hull stop":           engineEnsure, // sweeps containers down
	"hull forget":         engineEnsure, // brings the project down first
	"hull services add":   engineEnsure,
	"hull services start": engineEnsure,
	"hull services stop":  engineEnsure,
	"hull services rm":    engineEnsure,
	"hull cluster create": engineEnsure,

	// Nothing useful to show without the engine, so fail fast with a clean
	// message. Never launch Docker for a read: it is slow and surprising, and
	// `hull status` starting the thing it reports on would be absurd.
	"hull status": engineCheck,
	"hull logs":   engineCheck,

	// Deliberately engineNone even though they touch Docker.
	//
	// The diagnostics MUST run with the engine down, or you could never find out
	// it is down: a guard here would abort `hull doctor` before it reports the
	// very failure you ran it to see.
	//
	// The listers read their real content (names, paths, URLs) from disk, so
	// they stay useful with Docker closed. They mark running state unknown
	// rather than claiming everything is stopped, which is the confident wrong
	// answer they used to give.
	"hull doctor":             engineNone,
	"hull deps":               engineNone,
	"hull list":               engineNone,
	"hull services":           engineNone,
	"hull services list":      engineNone,
	"hull cluster":            engineNone,
	"hull cluster list":       engineNone,
	"hull cluster urls":       engineNone,
	"hull cluster route list": engineNone,
}

// applyEngineAnnotations stamps the declared mode onto each command. Called
// once from main() after every init() has registered its commands, and by the
// tests. Stamping annotations (rather than consulting the map at run time)
// keeps the requirement visible on the command itself.
func applyEngineAnnotations() {
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		mode, ok := engineModes[c.CommandPath()]
		if !ok {
			mode = engineNone
		}
		if c.Annotations == nil {
			c.Annotations = map[string]string{}
		}
		c.Annotations[engineAnnotation] = mode
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)
}

// enforceEngineRequirement runs before every command, and crucially before the
// daemon/in-process fork inside command bodies, so both paths are covered.
func enforceEngineRequirement(cmd *cobra.Command) error {
	switch cmd.Annotations[engineAnnotation] {
	case engineEnsure:
		return ensureDocker(cmd.Context())
	case engineCheck:
		return dockerx.EngineCheck(cmd.Context())
	default:
		return nil
	}
}

// ensureDocker makes sure the container engine is up, starting it when it is
// merely closed. Hull used to fail with "the container engine is not
// responding" when the fix was simply to launch Docker, so `hull up` looked
// broken on a machine where Docker was only closed.
//
// Progress is printed because a cold Docker Desktop start takes real time and a
// silent pause reads as a hang.
func ensureDocker(ctx context.Context) error {
	return dockerx.EnsureEngine(ctx, func(msg string) { fmt.Println(msg) })
}

// reportEngineState warns when Hull is up but Docker is not. `hull start` is
// about the daemon, so it must not silently launch Docker, but printing "Hull
// is running." while the engine is dead is the kind of half-truth that sends
// someone hunting through hosts files. Say it plainly instead.
func reportEngineState(ctx context.Context) {
	if err := dockerx.EngineCheck(ctx); err != nil {
		fmt.Println()
		fmt.Println("! Docker is not running, so projects cannot start.")
		fmt.Println("  Start Docker, then run: hull up")
	}
}
