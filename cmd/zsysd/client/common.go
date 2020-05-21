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
	c, err := zsys.NewZsysUnixSocketClient(config.DefaultSocket, log.GetLevel())
	if err != nil {
		return nil, fmt.Errorf(i18n.G("couldn't connect to zsys daemon: %v"), err)
	}
	return c, nil
}

// checkConn checks for unavailable service and unwrap any other rpc error to its message.
func checkConn(err error) error {
	if err != nil {
		st, _ := status.FromError(err)
		if st.Code() == codes.Unavailable {
			return fmt.Errorf(i18n.G("couldn't connect to zsys daemon: %v"), st.Message())
		}
		return errors.New(st.Message())
	}

	return nil
}

// contextWithResettableTimeout returns a context that can be cancelled manually reset to timeout value sending an element to the returned channel
func contextWithResettableTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc, chan<- struct{}) {
	ctx, cancel := context.WithCancel(ctx)
	reset := make(chan struct{})

	go func() {
		for {
			select {
			case <-time.After(timeout):
				log.Debugf(ctx, i18n.G("Didn't receive any information from service in %s"), timeout)
				cancel()
				break
			case <-reset:
			}
		}
	}()

	return ctx, cancel, reset
}
