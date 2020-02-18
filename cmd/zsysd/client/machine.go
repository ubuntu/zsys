package client

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/ubuntu/zsys"
	"github.com/ubuntu/zsys/cmd/zsysd/cmdhandler"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/streamlogger"
)

var (
	machineCmd = &cobra.Command{
		Use:   "machine COMMAND",
		Short: i18n.G("Machine management"),
		Args:  cmdhandler.SubcommandsRequiredWithSuggestions,
		Run:   cmdhandler.NoCmd,
	}

	showCmd = &cobra.Command{
		Use:   "show [MachineID]",
		Short: i18n.G("Shows the status of the machine."),
		Args:  cobra.MaximumNArgs(1),
		Run:   func(cmd *cobra.Command, args []string) { cmdErr = show(args) },
	}

	listCmd = &cobra.Command{
		Use:   "list",
		Short: i18n.G("List all the machines and basic information."),
		Args:  cobra.NoArgs,
		Run:   func(cmd *cobra.Command, args []string) { cmdErr = list(args) },
	}
)

var (
	fullInfo bool
)

func init() {
	rootCmd.AddCommand(machineCmd)
	machineCmd.AddCommand(showCmd)
	machineCmd.AddCommand(listCmd)

	showCmd.Flags().BoolVarP(&fullInfo, "full", "", false, i18n.G("Give more detail informations on each machine."))

	cmdhandler.RegisterAlias(listCmd, rootCmd)
	cmdhandler.RegisterAlias(showCmd, rootCmd)
}

func show(args []string) error {
	var machineID string
	if len(args) > 0 {
		machineID = args[0]
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(client.Ctx, config.DefaultClientTimeout)
	defer cancel()

	stream, err := client.MachineShow(ctx, &zsys.MachineShowRequest{MachineId: machineID, Full: fullInfo})

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
		fmt.Printf(r.GetMachineInfo())
	}

	return nil
}
func list(args []string) error {
	client, err := newClient()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(client.Ctx, config.DefaultClientTimeout)
	defer cancel()

	stream, err := client.MachineList(ctx, &zsys.Empty{})

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
		fmt.Printf(r.GetMachineList())
	}

	return nil
}
