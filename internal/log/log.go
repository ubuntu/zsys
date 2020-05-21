/*

Package log proxy logs to logrus logger and an optional io.Writer.
Both can have independent logging levels.

*/
package log

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"
)

type requestInfoKeyType string

const (
	requestInfoKey requestInfoKeyType = "logrequestinfo"
	reqIDFormat                       = "[[%s]] %s"
)

type requestInfo struct {
	id     string
	logger *logrus.Logger
}

// SetLevel sets default logger
func SetLevel(l logrus.Level) {
	setLevelLogger(logrus.StandardLogger(), l, false)
}

// GetLevel gets default logger level
func GetLevel() logrus.Level {
	return logrus.GetLevel()
}

// setLevelLogger sets given logger to level.
// "simplified" enables to ignore the TTY check in logrus and force color mode and not systemd printing.
func setLevelLogger(logger *logrus.Logger, l logrus.Level, simplified bool) {
	f := &logrus.TextFormatter{
		DisableLevelTruncation: true,
		DisableTimestamp:       true,
	}
	if simplified {
		f.ForceColors = true
	}

	logger.SetLevel(l)
	logger.SetFormatter(f)
}

// Debug logs a message at level Debug on the standard logger
// and may push to the stream referenced by ctx.
func Debug(ctx context.Context, args ...interface{}) {
	Debugf(ctx, "%s", args...)
}

// Debugf logs a message at level Debug on the standard logger
// and may push to the stream may push to the stream referenced by ctx.
func Debugf(ctx context.Context, format string, args ...interface{}) {
	if info, ok := ctx.Value(requestInfoKey).(*requestInfo); ok {
		info.logger.Debugf(format, args...)
		// for standard logger, save the id
		format = fmt.Sprintf(reqIDFormat, info.id, format)
	}
	logrus.Debugf(format, args...)
}

// Info logs a message at level Info on the standard logger
// and may push to the stream referenced by ctx.
func Info(ctx context.Context, args ...interface{}) {
	Infof(ctx, "%s", args...)
}

// Infof logs a message at level Info on the standard logger
// and may push to the stream may push to the stream referenced by ctx.
func Infof(ctx context.Context, format string, args ...interface{}) {
	if info, ok := ctx.Value(requestInfoKey).(*requestInfo); ok {
		info.logger.Infof(format, args...)
		// for standard logger, save the id
		format = fmt.Sprintf(reqIDFormat, info.id, format)
	}
	logrus.Infof(format, args...)
}

// RemotePrint logs a message that is only written on the remote
// client end stream referenced by ctx.
func RemotePrint(ctx context.Context, args ...interface{}) {
	RemotePrintf(ctx, "%s", args...)
}

// RemotePrintf logs a message that is only written on the remote
// client end stream referenced by ctx.
func RemotePrintf(ctx context.Context, format string, args ...interface{}) {
	if info, ok := ctx.Value(requestInfoKey).(*requestInfo); ok {
		info.logger.Out.Write([]byte(fmt.Sprintf(format, args...)))
	}
}

// Warning logs a message at level Warning on the standard logger
// and may push to the stream referenced by ctx.
func Warning(ctx context.Context, args ...interface{}) {
	Warningf(ctx, "%s", args...)
}

// Warningf logs a message at level Warning on the standard logger
// and may push to the stream may push to the stream referenced by ctx.
func Warningf(ctx context.Context, format string, args ...interface{}) {
	if info, ok := ctx.Value(requestInfoKey).(*requestInfo); ok {
		info.logger.Warningf(format, args...)
		// for standard logger, save the id
		format = fmt.Sprintf(reqIDFormat, info.id, format)
	}
	logrus.Warningf(format, args...)
}

// Error logs a message at level Error on the standard logger
// and may push to the stream referenced by ctx.
func Error(ctx context.Context, args ...interface{}) {
	Errorf(ctx, "%s", args...)
}

// Errorf logs a message at level Error on the standard logger
// and may push to the stream may push to the stream referenced by ctx.
func Errorf(ctx context.Context, format string, args ...interface{}) {
	if info, ok := ctx.Value(requestInfoKey).(*requestInfo); ok {
		info.logger.Errorf(format, args...)
		// for standard logger, save the id
		format = fmt.Sprintf(reqIDFormat, info.id, format)
	}
	logrus.Errorf(format, args...)
}
