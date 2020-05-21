package client

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/ubuntu/zsys"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/streamlogger"
)

var (
	versionCmd = &cobra.Command{
		Use:   "version",
		Short: i18n.G("Returns version of client and server"),
		Args:  cobra.NoArgs,
		Run:   func(cmd *cobra.Command, args []string) { cmdErr = getVersion() },
	}
)

func init() {
	rootCmd.AddCommand(versionCmd)
}

// getVersion returns the current server and client versions.
func getVersion() (err error) {
	fmt.Printf(i18n.G("zsysctl\t%s")+"\n", config.Version)

	client, err := newClient()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel, reset := contextWithResettableTimeout(client.Ctx, config.DefaultClientTimeout)
	defer cancel()

	stream, err := client.Version(ctx, &zsys.Empty{})
	if err = checkConn(err, reset); err != nil {
		return err
	}

	for {
		r, err := stream.Recv()
		if err == streamlogger.ErrLogMsg {
			reset <- struct{}{}
			continue
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		fmt.Printf(i18n.G("zsysd\t%s")+"\n", r.GetVersion())
	}

	return nil
}
