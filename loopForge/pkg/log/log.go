// Package log defines the structured Logger interface used across loopforge
// and provides adapters for common logging libraries.
package log

import "log/slog"

// Logger is a structured logger. Each method takes a message followed by
// alternating key-value pairs:
//
//	l.Info("agent transfer", "from", "triage", "to", "expert")
//
// *slog.Logger satisfies this interface out of the box.
type Logger interface {
	Debug(msg string, keysAndValues ...any)
	Info(msg string, keysAndValues ...any)
	Warn(msg string, keysAndValues ...any)
	Error(msg string, keysAndValues ...any)
}

// Default returns a Logger backed by slog.Default().
func Default() Logger {
	return slog.Default()
}

// Nop returns a Logger that silently discards all output.
func Nop() Logger {
	return nopLogger{}
}

type nopLogger struct{}

func (nopLogger) Debug(string, ...any) {}
func (nopLogger) Info(string, ...any)  {}
func (nopLogger) Warn(string, ...any)  {}
func (nopLogger) Error(string, ...any) {}

// sugaredLogger matches the subset of *zap.SugaredLogger used by FromSugared.
type sugaredLogger interface {
	Debugw(msg string, keysAndValues ...any)
	Infow(msg string, keysAndValues ...any)
	Warnw(msg string, keysAndValues ...any)
	Errorw(msg string, keysAndValues ...any)
}

type sugaredAdapter struct{ s sugaredLogger }

func (a sugaredAdapter) Debug(msg string, kv ...any) { a.s.Debugw(msg, kv...) }
func (a sugaredAdapter) Info(msg string, kv ...any)  { a.s.Infow(msg, kv...) }
func (a sugaredAdapter) Warn(msg string, kv ...any)  { a.s.Warnw(msg, kv...) }
func (a sugaredAdapter) Error(msg string, kv ...any) { a.s.Errorw(msg, kv...) }

// FromSugared wraps a *zap.SugaredLogger (or any logger with
// Debugw/Infow/Warnw/Errorw methods) into a Logger.
//
// Usage:
//
//	log.FromSugared(zapLogger.Sugar())
func FromSugared(s sugaredLogger) Logger {
	return sugaredAdapter{s}
}
