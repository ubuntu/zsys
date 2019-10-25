package client

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/ubuntu/zsys"
	"github.com/ubuntu/zsys/internal/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// requireSubcommand is a no-op command which return an error message to trigger
// a command usage error.
func requireSubcommand(cmd *cobra.Command, args []string) error {
	return fmt.Errorf("%s requires a subcommand", cmd.Name())
}

// newClient returns a new zsys client object
func newClient() (*zsys.ZsysLogClient, error) {
	// TODO: allow change socket address
	c, err := zsys.NewZsysUnixSocketClient(zsys.DefaultSocket, log.GetLevel())
	if err != nil {
		return nil, fmt.Errorf("couldn't connect to zsys daemon: %v", err)
	}
	return c, nil
}

func checkConn(err error) error {
	if err != nil {
		st, _ := status.FromError(err)
		if st.Code() == codes.Unavailable {
			return fmt.Errorf("couldn't connect to zsys daemon: %v", st.Message())
		}
		return errors.New(st.Message())
	}

	return nil
}
