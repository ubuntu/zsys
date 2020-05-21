package streamlogger

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/sirupsen/logrus"
	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// ErrLogMsg allows detecting when a recv message was only logs to client, consumed by the interceptor.
var ErrLogMsg = errors.New(i18n.G("message was log"))

// NewClientCtx creates a requester ID and attach it to returned context.
func NewClientCtx(ctx context.Context, level logrus.Level) context.Context {
	requesterID := "unknown"
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		logrus.Warningf(i18n.G("couldn't generate request id, setting to %q: %v"), requesterID, err)
	} else {
		requesterID = fmt.Sprintf("%x", b[0:])
	}

	return metadata.NewOutgoingContext(ctx, metadata.Pairs(
		metaRequesterIDKey, requesterID,
		metaLevelKey, level.String()))
}

// ClientRequestLogInterceptor ensure that the stream get a valid requestID from the service in headers and
// consumes logs by printing them.
func ClientRequestLogInterceptor(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn,
	method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {

	s, err := streamer(ctx, desc, cc, method, opts...)
	if err != nil {
		return nil, err
	}

	return &clientRequestLogStream{s, sync.Once{}, method}, nil
}

// clientRequestLogStream is a stream wrapper sending immediately and once Header() the client and consuming
// logs by printing them.
type clientRequestLogStream struct {
	grpc.ClientStream
	once   sync.Once
	method string
}

// SendMsg wraps internal SendMsg client string, and on connection, call once Header() to get
// the Request ID from the server.
func (w *clientRequestLogStream) SendMsg(m interface{}) (errFn error) {
	if err := w.ClientStream.SendMsg(m); err != nil {
		return err
	}
	errFn = nil
	w.once.Do(func() {
		header, err := w.Header()
		if err != nil {
			errFn = fmt.Errorf(i18n.G("failed to get header from stream: %w"), err)
			return
		}
		// Read metadata from server's header.
		id, ok := header[metaRequestIDKey]
		if !ok {
			errFn = errors.New(i18n.G("no request ID found on server header"))
			return
		}
		log.Debugf(context.Background(), i18n.G("%s() call logged as %s"), w.method, id)
	})

	return errFn
}

type getLogger interface {
	GetLog() string
}

// RecvMsg wraps internal RecvMsg client string to consum logs and prints them if any before sending the result.
// It also decode the error if any.
func (w *clientRequestLogStream) RecvMsg(m interface{}) (errFn error) {
	err := w.ClientStream.RecvMsg(m)
	if err == io.EOF {
		return err
	}
	if err != nil {
		switch st := status.Convert(err); st.Code() {
		case codes.Canceled:
			return context.Canceled
		case codes.Unknown:
			return errors.New(st.Message())
		}
		return err
	}

	r, isLogMsg := m.(getLogger)
	// Not a log message, pass it on
	if !isLogMsg {
		return nil
	}

	l := r.GetLog()
	// Not a log message, pass it on
	if l == "" {
		return nil
	}

	if l != log.PingLogMessage {
		fmt.Fprint(os.Stderr, l)
	}
	return ErrLogMsg
}
