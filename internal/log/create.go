package log

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"

	"github.com/sirupsen/logrus"
	"github.com/ubuntu/zsys/internal/i18n"
)

const (
	// DefaultLevel only prints warning and errors.
	DefaultLevel = logrus.WarnLevel
	// InfoLevel is signaling system information like global calls.
	InfoLevel = logrus.InfoLevel
	// DebugLevel gives fine-grained details about executions.
	DebugLevel = logrus.DebugLevel
)

const (
	defaultRequestID = "unknown"
)

// ContextWithLogger returns a context which will log to the writer.
// Level is based on metadata information from the ctx request.
// A generated request ID is added to a requester ID and attached to the context
func ContextWithLogger(ctx context.Context, requesterID, level string, w io.Writer) (newCtx context.Context, err error) {
	// Generate a request id.
	requestID := defaultRequestID
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		logrus.Warningf(i18n.G("couldn't generate request id, setting to %q: %v"), defaultRequestID, err)
	} else {
		requestID = fmt.Sprintf("%x", b[0:])
	}
	id := fmt.Sprintf("%s:%s", requesterID, requestID)

	// Get logging level.
	var logLevel logrus.Level
	if logLevel, err = logrus.ParseLevel(level); err != nil {
		logrus.Warningf(i18n.G("invalid log level requested. Using default: %v"), err)
	}

	// Associate the context with a new logger, for which output is the io.Writer.
	logger := logrus.New()
	logger.SetOutput(w)
	// ignore the TTY check in logrus and force color mode and not systemd printing format.
	setLevelLogger(logger, logLevel, true)

	return context.WithValue(ctx, requestInfoKey, &requestInfo{
		id:     id,
		logger: logger,
	}), nil
}

// IDFromContext returns current request log id from context
func IDFromContext(ctx context.Context) (string, error) {
	info, ok := ctx.Value(requestInfoKey).(*requestInfo)
	if !ok {
		return "", errors.New(i18n.G("no request ID attached to this context"))
	}

	return info.id, nil
}
