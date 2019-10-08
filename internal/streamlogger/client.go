package streamlogger

import (
	"context"
	"crypto/rand"
	"fmt"
	"sync"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/ubuntu/zsys/internal/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// NewClientCtx creates a requester ID and attach it to returned context.
func NewClientCtx(ctx context.Context) context.Context {
	requesterID := "unknown"
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		logrus.Warningf("couldn't generate request id, setting to %q: %v", requesterID, err)
	} else {
		requesterID = fmt.Sprintf("%x", b[0:])
	}

	return metadata.NewOutgoingContext(ctx, metadata.Pairs(
		metaRequesterIDKey, requesterID,
		metaLevelKey, log.DebugLevel.String()))
}

// ClientRequestIDInterceptor ensure that the stream get a valid requestID from the service in headers.
func ClientRequestIDInterceptor(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn,
	method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {

	s, err := streamer(ctx, desc, cc, method, opts...)
	if err != nil {
		return nil, err
	}

	return &clientRequestIDStream{s, sync.Once{}, method}, nil
}

// clientRequestIDStream is a stream wrapper sending immediately and once Header() the client.
type clientRequestIDStream struct {
	grpc.ClientStream
	once   sync.Once
	method string
}

// SendMsg wraps internal SendMsg client string, and on connection, call once Header() to get
// the Request ID from the server.
func (w *clientRequestIDStream) SendMsg(m interface{}) (errFn error) {
	if err := w.ClientStream.SendMsg(m); err != nil {
		return err
	}
	errFn = nil
	w.once.Do(func() {
		header, err := w.Header()
		if err != nil {
			errFn = fmt.Errorf("failed to get header from stream: %w", err)
			return
		}
		// Read metadata from server's header.
		id, ok := header[metaRequestIDKey]
		if !ok {
			errFn = errors.New("no request ID found on server header")
			return
		}
		log.Debugf(context.Background(), "%s() call logged as %s", w.method, id)
	})

	return errFn
}
