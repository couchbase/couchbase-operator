package logging

import (
	"os"
	"strconv"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	zapf "sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// This is done for us by controller runtime v0.8.0, however the changes to
// the v0.18.0 client libraries are quite large, so do it manually for now.
type LogLevel struct {
	Level zapcore.Level
}

func (l *LogLevel) Type() string {
	return "logLevel"
}

func (l *LogLevel) String() string {
	return l.Level.String()
}

func (l *LogLevel) Set(s string) error {
	switch s {
	case "debug":
		l.Level = zapcore.DebugLevel
	case "info":
		l.Level = zapcore.InfoLevel
	case "error":
		l.Level = zapcore.ErrorLevel
	default:
		i, err := strconv.Atoi(s)
		if err != nil {
			return err
		}

		l.Level = zapcore.Level(-i)
	}

	return nil
}

// newLogger creates a zap logger.
func New(l zapcore.Level) logr.Logger {
	encoder := zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig())

	level := zap.NewAtomicLevelAt(l)

	stacktraceLevel := zap.NewAtomicLevelAt(zap.ErrorLevel)

	// Note that we don't use the controller-runtime version, that adds a
	// sampler by default that causes a panic when we use debug levels lower
	// than -1 (e.g. log.V(1))
	return zapr.NewLogger(zap.New(zapcore.NewCore(&zapf.KubeAwareEncoder{Encoder: encoder}, os.Stderr, level)).WithOptions(zap.AddStacktrace(stacktraceLevel)))
}
