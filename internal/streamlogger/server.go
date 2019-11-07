package streamlogger

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/sirupsen/logrus"
	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// StreamLogger is an interface to associate a writer (for message passing) to a stream
type StreamLogger interface {
	io.Writer
	grpc.ServerStream
}

// AddLogger initializes a stream by checking log level, send headers back to client
// and return a context for further logging.
func AddLogger(stream StreamLogger, funcName string) (context.Context, error) {
	ctx := stream.Context()
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, errors.New(i18n.G("invalid metadata for incoming request"))
	}

	// Get requester ID.
	requesterIDInfo, ok := md[metaRequesterIDKey]
	if !ok {
		return nil, errors.New(i18n.G("missing RequesterIDKey for incoming request"))
	}
	if len(requesterIDInfo) != 1 {
		return nil, fmt.Errorf(i18n.G("invalid RequesterIDKey for incoming request: %q"), requesterIDInfo)
	}

	// Get log level.
	levelInfo, ok := md[metaLevelKey]
	if !ok || len(levelInfo) != 1 {
		return nil, fmt.Errorf(i18n.G("invalid logLevelKey metadata for incoming request: %q"), levelInfo)
	}
	var err error
	if ctx, err = log.ContextWithLogger(ctx, requesterIDInfo[0], levelInfo[0], stream); err != nil {
		return ctx, fmt.Errorf(i18n.G("this request has invalid metadata: %w"), err)
	}

	id, err := log.IDFromContext(ctx)
	if err != nil {
		return ctx, errors.New(i18n.G("this request isn't associate with a valid id: reject"))
	}
	logrus.Infof(i18n.G("new incoming request %s() for %q"), funcName, id)

	if err := stream.SendHeader(metadata.New(map[string]string{metaRequestIDKey: id})); err != nil {
		return ctx, fmt.Errorf(i18n.G("couldn't send headers: %w"), err)
	}

	return ctx, nil
}

type requestTracker interface {
	TrackRequest() func()
}

// ServerIdleTimeoutInterceptor adds a call to reset the server stream timeout if available.
func ServerIdleTimeoutInterceptor(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	// Stop idling timeout and restart with resetting it in defer statement
	if s, ok := srv.(requestTracker); ok {
		defer s.TrackRequest()()
	}
	return handler(srv, ss)
}
