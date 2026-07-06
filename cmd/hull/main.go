// Command hull is the Hull CLI. It is hybrid by design: when a daemon
// is running it drives it over the local API (so the CLI and GUI share one
// writer and one view of state); when none is running it executes the same
// engine code in-process (the headless guarantee). --no-daemon forces the
// in-process path. Run `hull --help` for the command set.
package main
