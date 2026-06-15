/*
Copyright 2021-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package logging

import (
	"testing"

	"go.uber.org/zap/zapcore"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func TestNewWithLogLevel(t *testing.T) {
	tests := []struct {
		name         string
		logLevel     string
		defaultLevel zapcore.Level
		checkV       int // The V-level we expect to be enabled
		checkNotV    int // The higher V-level we expect to be disabled
	}{
		{
			name:         "Debug level",
			logLevel:     "debug",
			defaultLevel: zapcore.InfoLevel,
			checkV:       1,
			checkNotV:    2,
		},
		{
			name:         "Info level",
			logLevel:     "info",
			defaultLevel: zapcore.DebugLevel,
			checkV:       0,
			checkNotV:    1,
		},
		{
			name:         "Error level",
			logLevel:     "error",
			defaultLevel: zapcore.InfoLevel,
			checkV:       -1, // special case: Error disables Info V(0)
			checkNotV:    0,
		},
		{
			name:         "Level 2",
			logLevel:     "2",
			defaultLevel: zapcore.InfoLevel,
			checkV:       2,
			checkNotV:    3,
		},
		{
			name:         "Fallback to default with invalid input",
			logLevel:     "invalid-string",
			defaultLevel: zapcore.InfoLevel,
			checkV:       0,
			checkNotV:    1,
		},
		{
			name:         "Fallback to default with empty input",
			logLevel:     "",
			defaultLevel: zapcore.InfoLevel,
			checkV:       0,
			checkNotV:    1,
		},
		{
			name:         "Sanitization check (mixed case with spaces)",
			logLevel:     "  DeBuG  ",
			defaultLevel: zapcore.InfoLevel,
			checkV:       1,
			checkNotV:    2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &Options{
				Options: zap.Options{
					Level: tt.defaultLevel,
				},
			}

			logger := opts.NewWithLogLevel(tt.logLevel)

			if tt.checkV >= 0 && !logger.V(tt.checkV).Enabled() {
				t.Errorf("expected V(%d) to be enabled for log level %q", tt.checkV, tt.logLevel)
			}

			if logger.V(tt.checkNotV).Enabled() {
				t.Errorf("expected V(%d) to be disabled for log level %q", tt.checkNotV, tt.logLevel)
			}
		})
	}
}
