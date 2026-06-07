// Package logx centralizes diagnostic/progress logging for the CLI and bot.
//
// It wraps zerolog. Human-facing command *results* (user lists, links,
// subscription messages, version) must NOT go through logx — those belong on
// stdout via fmt. logx writes to stderr so logs never pollute piped output.
package logx

import (
	"os"
	"time"

	"github.com/rs/zerolog"
)

// L is the active logger. Replaced by Setup; safe to read before Setup runs
// (defaults to a pretty console writer at info level).
var L = newConsole(zerolog.InfoLevel)

func newConsole(level zerolog.Level) zerolog.Logger {
	cw := zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: "15:04:05"}
	return zerolog.New(cw).Level(level).With().Timestamp().Logger()
}

// Setup configures the global logger. When jsonOut is true, logs are emitted as
// JSON (suitable for journald/services); otherwise a colorized console format.
// When verbose is true, debug-level messages are shown.
func Setup(jsonOut, verbose bool) {
	level := zerolog.InfoLevel
	if verbose {
		level = zerolog.DebugLevel
	}
	if jsonOut {
		zerolog.TimeFieldFormat = time.RFC3339
		L = zerolog.New(os.Stderr).Level(level).With().Timestamp().Logger()
		return
	}
	L = newConsole(level)
}

func Debugf(format string, a ...interface{}) { L.Debug().Msgf(format, a...) }
func Infof(format string, a ...interface{})  { L.Info().Msgf(format, a...) }
func Warnf(format string, a ...interface{})  { L.Warn().Msgf(format, a...) }
func Errf(format string, a ...interface{})   { L.Error().Msgf(format, a...) }

// Phase logs a high-level section header.
func Phase(title string) { L.Info().Msg(title) }

// Step logs a numbered progress step ("[n/total] msg").
func Step(n, total int, msg string) { L.Info().Msgf("[%d/%d] %s", n, total, msg) }
