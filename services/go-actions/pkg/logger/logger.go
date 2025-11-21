package logger

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger wraps zap.SugaredLogger with debug mode support
type Logger struct {
	*zap.SugaredLogger
	debugMode bool
}

// New creates a new logger instance with debug mode based on environment variable
func New() *Logger {
	debugMode := os.Getenv("DEBUG") == "true" || os.Getenv("DEBUG") == "1"
	return newLogger(debugMode)
}

// NewWithDebug creates a logger with explicit debug mode setting
func NewWithDebug(debug bool) *Logger {
	return newLogger(debug)
}

// NewDevelopment creates a development logger with more verbose output
func NewDevelopment() *Logger {
	config := zap.NewDevelopmentConfig()
	config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder

	logger, err := config.Build()
	if err != nil {
		panic(err)
	}

	return &Logger{
		SugaredLogger: logger.Sugar(),
		debugMode:     true,
	}
}

func newLogger(debugMode bool) *Logger {
	config := zap.NewProductionConfig()
	config.EncoderConfig.TimeKey = "timestamp"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	// Enable debug level if debug mode is on
	if debugMode {
		config.Level = zap.NewAtomicLevelAt(zapcore.DebugLevel)
	} else {
		config.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
	}

	logger, err := config.Build()
	if err != nil {
		panic(err)
	}

	return &Logger{
		SugaredLogger: logger.Sugar(),
		debugMode:     debugMode,
	}
}

// IsDebugEnabled returns true if debug mode is enabled
func (l *Logger) IsDebugEnabled() bool {
	return l.debugMode
}

// Debugf logs a debug message (only if debug mode is enabled)
func (l *Logger) Debugf(template string, args ...interface{}) {
	if l.debugMode {
		l.SugaredLogger.Debugf(template, args...)
	}
}
