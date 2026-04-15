package logger_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"seraglf/internal/logger"
)

func TestSetup_Levels(t *testing.T) {
	tests := []struct {
		name  string
		level string
	}{
		{"debug level", "debug"},
		{"info level", "info"},
		{"warn level", "warn"},
		{"warning level", "warning"},
		{"error level", "error"},
		{"unknown defaults to info", "unknown"},
		{"empty defaults to info", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := logger.Setup(logger.Config{Level: tt.level, Format: "text"})
			assert.NotNil(t, log)
		})
	}
}

func TestSetup_Formats(t *testing.T) {
	tests := []struct {
		name   string
		format string
	}{
		{"json format", "json"},
		{"text format", "text"},
		{"unknown defaults to text", "unknown"},
		{"empty defaults to text", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := logger.Setup(logger.Config{Level: "info", Format: tt.format})
			assert.NotNil(t, log)
		})
	}
}
