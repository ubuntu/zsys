package client

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/ubuntu/zsys"
	"github.com/ubuntu/zsys/cmd/zsysd/cmdhandler"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/streamlogger"
)

var (
	serviceCmd = &cobra.Command{
		Use:   "service COMMAND",
		Short: i18n.G("Service management"),
		Args:  cmdhandler.SubcommandsRequiredWithSuggestions,
		Run:   cmdhandler.NoCmd,
	}
	daemonstopCmd = &cobra.Command{
		Use:   "stop",
		Short: i18n.G("stops zsys daemon."),
		Args:  cobra.NoArgs,
		Run:   func(cmd *cobra.Command, args []string) { cmdErr = daemonStop() },
	}
	servicedumpCmd = &cobra.Command{
		Use:   "dump",
		Short: i18n.G("Dumps the current state of zsys."),
		Args:  cobra.NoArgs,
		Run:   func(cmd *cobra.Command, args []string) { cmdErr = dumpService() },
	}
	logginglevelCmd = &cobra.Command{
		Use:   "loglevel 0|1|2",
		Short: i18n.G("Sets the logging level of the daemon."),
		Args:  cobra.ExactArgs(1),
		Run:   func(cmd *cobra.Command, args []string) { cmdErr = loggingLevel(args) },
	}
)

func init() {
	rootCmd.AddCommand(serviceCmd)
	serviceCmd.AddCommand(daemonstopCmd)
	serviceCmd.AddCommand(servicedumpCmd)
	serviceCmd.AddCommand(logginglevelCmd)
}

func daemonStop() error {
	client, err := newClient()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(client.Ctx, config.DefaultClientTimeout)
	defer cancel()

	stream, err := client.DaemonStop(ctx, &zsys.Empty{})
	if err = checkConn(err); err != nil {
		return err
	}

	for {
		_, err := stream.Recv()
		if err == streamlogger.ErrLogMsg {
			continue
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}

	return nil
}

func dumpService() error {
	client, err := newClient()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(client.Ctx, config.DefaultClientTimeout)
	defer cancel()

	stream, err := client.DumpStates(ctx, &zsys.Empty{})
	if err = checkConn(err); err != nil {
		return err
	}

	for {
		r, err := stream.Recv()
		if err == streamlogger.ErrLogMsg {
			continue
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		fmt.Println(r.GetStates())
	}

	return nil
}

func loggingLevel(args []string) error {
	level, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf(i18n.G("logging level must be an integer: %v"), err)
	}

	client, err := newClient()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(client.Ctx, config.DefaultClientTimeout)
	defer cancel()

	stream, err := client.LoggingLevel(ctx, &zsys.LoggingLevelRequest{Logginglevel: int32(level)})
	if err = checkConn(err); err != nil {
		return err
	}

	for {
		_, err := stream.Recv()
		if err == streamlogger.ErrLogMsg {
			continue
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}

	return nil
}
