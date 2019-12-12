package daemon

import (
	"context"
	"time"

	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/log"
)

type idler struct {
	timeout time.Duration

	requestsInFlights int
	newRequest        chan struct{}
	reset             chan struct{}
}

func newIdler(timeout time.Duration) idler {
	return idler{
		timeout: timeout,

		newRequest: make(chan struct{}),
		reset:      make(chan struct{}),
	}
}

func (i idler) addRequest() {
	i.newRequest <- struct{}{}
}

func (i idler) endRequest() {
	i.reset <- struct{}{}
}

func (i idler) start(s *Server) {
	defer s.Stop()
	t := time.NewTimer(i.timeout)

	for {
		select {
		case <-t.C:
			log.Debug(context.Background(), i18n.G("Idle timeout expired"))
			return
		case <-i.newRequest:
			i.requestsInFlights++
			// Stop can return false if the timeout has fired OR if it's already stopped. Use requestsInFlights
			// to only drain the timeout channel if the timeout has already fired.
			if i.requestsInFlights == 1 && !t.Stop() {
				<-t.C
			}
		case <-i.reset:
			i.requestsInFlights--
			if i.requestsInFlights > 0 {
				continue
			}
			t.Reset(i.timeout)
		}
	}
}
