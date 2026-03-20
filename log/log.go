package log

import (
	"os"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var logger *zap.SugaredLogger

// Init initializes the global logger.
func Init(level string, errorLogPath string) error {
	var zapLevel zapcore.Level
	switch strings.ToLower(level) {
	case "debug":
		zapLevel = zapcore.DebugLevel
	case "info":
		zapLevel = zapcore.InfoLevel
	case "warn":
		zapLevel = zapcore.WarnLevel
	case "error":
		zapLevel = zapcore.ErrorLevel
	default:
		zapLevel = zapcore.InfoLevel
	}

	encoderCfg := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	// Console output
	consoleCore := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderCfg),
		zapcore.AddSync(os.Stdout),
		zapLevel,
	)

	cores := []zapcore.Core{consoleCore}

	// File output if configured
	if errorLogPath != "" {
		f, err := os.OpenFile(errorLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		fileCore := zapcore.NewCore(
			zapcore.NewJSONEncoder(encoderCfg),
			zapcore.AddSync(f),
			zapLevel,
		)
		cores = append(cores, fileCore)
	}

	core := zapcore.NewTee(cores...)
	l := zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1))
	logger = l.Sugar()
	return nil
}

func Sync() {
	if logger != nil {
		_ = logger.Sync()
	}
}

func Info(msg string, keysAndValues ...interface{})  { logger.Infow(msg, keysAndValues...) }
func Error(msg string, keysAndValues ...interface{}) { logger.Errorw(msg, keysAndValues...) }
func Warn(msg string, keysAndValues ...interface{})  { logger.Warnw(msg, keysAndValues...) }
func Debug(msg string, keysAndValues ...interface{}) { logger.Debugw(msg, keysAndValues...) }
func Fatal(msg string, keysAndValues ...interface{}) { logger.Fatalw(msg, keysAndValues...) }
