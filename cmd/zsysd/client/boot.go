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
	printModifiedBoot bool

	bootCmd = &cobra.Command{
		Use:    "boot COMMAND",
		Short:  i18n.G("Enssure that the right datasets are ready to be mounted and committed during early boot"),
		Hidden: true,
		Args:   cmdhandler.SubcommandsRequiredWithSuggestions,
		Run:    cmdhandler.NoCmd,
	}
	bootPrepareCmd = &cobra.Command{
		Use:   "prepare",
		Short: i18n.G("Prepare boot by ensuring correct system and user datasets are switched on and off"),
		Args:  cobra.NoArgs,
		Run:   func(cmd *cobra.Command, args []string) { cmdErr = bootPrepare(printModifiedBoot) },
	}
	bootCommitCmd = &cobra.Command{
		Use:   "commit",
		Short: i18n.G("Commit system and user datasets states as a successful boot"),
		Args:  cobra.NoArgs,
		Run:   func(cmd *cobra.Command, args []string) { cmdErr = bootCommit(printModifiedBoot) },
	}
)

func init() {
	bootCmd.PersistentFlags().BoolVarP(&printModifiedBoot, "print-changes", "p", false, i18n.G("Display if any zfs datasets have been modified to boot"))
	rootCmd.AddCommand(bootCmd)
	bootCmd.AddCommand(bootPrepareCmd)
	bootCmd.AddCommand(bootCommitCmd)
}

func bootPrepare(printModifiedBoot bool) (err error) {
	client, err := newClient()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(client.Ctx, config.DefaultClientTimeout)
	defer cancel()

	stream, err := client.PrepareBoot(ctx, &zsys.Empty{})
	if err = checkConn(err); err != nil {
		return err
	}

	var changed bool
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

		changed = r.GetChanged()
	}

	if printModifiedBoot && changed {
		fmt.Println(config.ModifiedBoot)
	} else if printModifiedBoot && !changed {
		fmt.Println(config.NoModifiedBoot)
	}

	return nil
}

func bootCommit(printModifiedBoot bool) (err error) {
	client, err := newClient()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(client.Ctx, config.DefaultClientTimeout)
	defer cancel()

	stream, err := client.CommitBoot(ctx, &zsys.Empty{})
	if err = checkConn(err); err != nil {
		return err
	}

	var changed bool
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

		changed = r.GetChanged()
	}

	if printModifiedBoot && changed {
		fmt.Println(config.ModifiedBoot)
	} else if printModifiedBoot && !changed {
		fmt.Println(config.NoModifiedBoot)
	}

	return nil
}
