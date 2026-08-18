// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package cli

import (
	"log/slog"

	logger "github.com/yousysadmin/go-logger"

	"github.com/yousysadmin/mailyard/internal/core/env"
)

// buildLogger turns the logging config into exactly one sink.
//
// Exactly one, because building a console sink and a JSON sink
// unconditionally has both writing to stdout on a default install and
// every line appearing twice - with logging.format and logging.color
// read by nothing.
//
// One sink, doing what the config says. Two destinations (pretty for a
// human, JSON on disk for a shipper) is reasonable to want but needs a
// real list-of-sinks config, not a hardcoded pair.
func buildLogger(cfg env.LoggingConfig) (*slog.Logger, error) {
	format := cfg.Format
	if format == "" {
		format = logger.FormatJSON
	}

	output := cfg.Output
	if output == "" {
		output = logger.OutputStdout
	}

	// Color is requested, not forced: the library honors it only when
	// the resolved output is an interactive terminal, so a file or a
	// piped stdout still gets clean, parseable text.
	return logger.New(logger.Sink{
		Level:  cfg.Level,
		Output: output,
		Format: format,
		Color:  cfg.Color && format == logger.FormatText,
	})
}
