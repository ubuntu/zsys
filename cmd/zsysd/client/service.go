package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/ubuntu/zsys"
	"github.com/ubuntu/zsys/cmd/zsysd/cmdhandler"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/log"
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
		Short: i18n.G("Stops zsys daemon."),
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
	refreshCmd = &cobra.Command{
		Use:   "refresh",
		Short: i18n.G("Refreshes machines states."),
		Args:  cobra.NoArgs,
		Run:   func(cmd *cobra.Command, args []string) { cmdErr = refresh() },
	}
	traceCmd = &cobra.Command{
		Use:   "trace",
		Short: i18n.G("Start profiling until you exit this command yourself or when duration is done. Default is CPU profiling with a 30s timeout."),
		Args:  cobra.NoArgs,
		Run:   func(cmd *cobra.Command, args []string) { cmdErr = trace() },
	}
	statusCmd = &cobra.Command{
		Use:   "status",
		Short: i18n.G("Shows the status of the daemon."),
		Args:  cobra.NoArgs,
		Run:   func(cmd *cobra.Command, args []string) { cmdErr = daemonStatus() },
	}
	reloadCmd = &cobra.Command{
		Use:   "reload",
		Short: i18n.G("Reloads daemon configuration."),
		Args:  cobra.NoArgs,
		Run:   func(cmd *cobra.Command, args []string) { cmdErr = reloadConfig() },
	}
	gcCmd = &cobra.Command{
		Use:   "gc",
		Short: i18n.G("Run daemon state saves garbage collection."),
		Args:  cobra.NoArgs,
		Run:   func(cmd *cobra.Command, args []string) { cmdErr = gc() },
	}
)

var (
	traceOutput   string
	traceType     string
	traceDuration int
)

func init() {
	rootCmd.AddCommand(serviceCmd)
	serviceCmd.AddCommand(daemonstopCmd)
	serviceCmd.AddCommand(servicedumpCmd)
	serviceCmd.AddCommand(logginglevelCmd)
	serviceCmd.AddCommand(refreshCmd)
	serviceCmd.AddCommand(traceCmd)
	serviceCmd.AddCommand(reloadCmd)
	serviceCmd.AddCommand(gcCmd)

	traceCmd.Flags().StringVarP(&traceOutput, "output", "o", "", i18n.G("Dump the trace to a file. Default is ./zsys.<trace-type>.pprof"))
	traceCmd.Flags().StringVarP(&traceType, "type", "t", "cpu", i18n.G("Type of profiling cpu or mem. Default is cpu."))
	traceCmd.Flags().IntVarP(&traceDuration, "duration", "", 30, i18n.G("Duration of the capture. Default is 30 seconds."))

	serviceCmd.AddCommand(statusCmd)

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

func refresh() error {
	client, err := newClient()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(client.Ctx, config.DefaultClientTimeout)
	defer cancel()

	stream, err := client.Refresh(ctx, &zsys.Empty{})
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

func trace() error {
	switch traceType {
	case "":
		traceType = "cpu"
	case "cpu":
	case "mem":
	default:
		return fmt.Errorf(i18n.G("Unsupported trace type: %s"), traceType)
	}

	if traceOutput == "" {
		dir, err := os.Getwd()
		if err != nil {
			return err
		}
		traceOutput = filepath.Join(dir, fmt.Sprintf("zsys.%s.pprof", traceType))
	}
	f, err := os.Create(traceOutput)
	if err != nil {
		return fmt.Errorf(i18n.G("Couldn't open trace file %s: %v"), traceOutput, err)
	}
	defer f.Close()

	if traceDuration < 0 {
		return errors.New(i18n.G("duration must be a positive integer"))
	}

	client, err := newClient()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx := client.Ctx
	// TODO: ctrl+C handling and receive everything on close. Control with timeout
	/*if traceDuration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Second*time.Duration(traceDuration))
		defer cancel()
		log.Infof(ctx, "Enabling %s profiling for %ds.", traceType, traceDuration)
	} else {
		log.Infof(ctx, "Enabling %s profiling. Press CTRL+C to terminate.", traceType)
	}*/

	log.Infof(ctx, "Trace saved to %s", traceOutput)

	stream, err := client.Trace(ctx, &zsys.TraceRequest{
		Type:     traceType,
		Duration: int32(traceDuration),
	})

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

		b := r.GetTrace()
		if _, err := f.Write(b); err != nil {
			return fmt.Errorf(i18n.G("Couldn't write to file: %v"), err)
		}
	}

	return nil
}

func daemonStatus() error {
	client, err := newClient()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(client.Ctx, config.DefaultClientTimeout)
	defer cancel()

	stream, err := client.Status(ctx, &zsys.Empty{})
	if err = checkConn(err); err != nil {
		return err
	}

	for {
		_, err := stream.Recv()
		if err == streamlogger.ErrLogMsg {
			continue
		}
		if err == io.EOF {
			fmt.Println("OK")
			break
		}
		if err != nil {
			return err
		}
	}

	return nil
}
func reloadConfig() error {
	client, err := newClient()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(client.Ctx, config.DefaultClientTimeout)
	defer cancel()

	stream, err := client.Reload(ctx, &zsys.Empty{})
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

func gc() error {
	client, err := newClient()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(client.Ctx, config.DefaultClientTimeout)
	defer cancel()

	stream, err := client.GC(ctx, &zsys.Empty{})
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
