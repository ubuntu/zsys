package client

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ubuntu/zsys"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// newClient returns a new zsys client object
func newClient() (*zsys.ZsysLogClient, error) {
	// TODO: allow change socket address
	c, err := zsys.NewZsysUnixSocketClient(config.SocketPath(), log.GetLevel())
	if err != nil {
		return nil, fmt.Errorf(i18n.G("couldn't connect to zsys daemon: %v"), err)
	}
	return c, nil
}

// checkConn checks for unavailable service and unwrap any other rpc error to its message and reset timeout timer.
func checkConn(err error, reset chan<- struct{}) error {
	if err != nil {
		switch st := status.Convert(err); st.Code() {
		case codes.Unavailable:
			return fmt.Errorf(i18n.G("couldn't connect to zsys daemon: %v"), st.Message())
		case codes.Canceled:
			return context.Canceled
		default:
			return errors.New(st.Message())
		}
	}

	reset <- struct{}{}
	return nil
}

// contextWithResettableTimeout returns a cancellable context that can be manually reset to timeout value
// when sending an element to the returned channel.
// Note that the first request is longer, letting the service accepting the new request (and optionally loading).
func contextWithResettableTimeout(ctx context.Context, requestTimeout time.Duration) (context.Context, context.CancelFunc, chan<- struct{}) {
	ctx, cancel := context.WithCancel(ctx)
	reset := make(chan struct{})

	// First request can be longer until the service is ready and send a first reset on connexion ack
	timeout := config.DefaultClientWaitOnServiceReady

	go func() {
		for {
			select {
			case <-time.After(timeout):
				log.Debugf(ctx, i18n.G("Didn't receive any information from service in %s"), timeout)
				cancel()
				break
			case <-reset:
			}
			timeout = requestTimeout
		}
	}()

	return ctx, cancel, reset
}
