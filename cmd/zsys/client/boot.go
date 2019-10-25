package client

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/ubuntu/zsys"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/streamlogger"
	"github.com/ubuntu/zsys/internal/zfs"
)

var (
	printModifiedBoot bool

	bootCmd = &cobra.Command{
		Use:    "boot COMMAND",
		Short:  "Ensure that the right datasets are ready to be mounted and committed during early boot",
		Hidden: true,
		RunE:   requireSubcommand,
	}
	bootPrepareCmd = &cobra.Command{
		Use:   "prepare",
		Short: "Prepare boot by ensuring correct system and user datasets are switched on and off",
		Args:  cobra.NoArgs,
		Run:   func(cmd *cobra.Command, args []string) { cmdErr = bootPrepare(printModifiedBoot) },
	}
	bootCommitCmd = &cobra.Command{
		Use:   "commit",
		Short: "Commit system and user datasets states as a successful boot",
		Args:  cobra.NoArgs,
		Run:   func(cmd *cobra.Command, args []string) { cmdErr = bootCommit(printModifiedBoot) },
	}
)

const (
	modifiedBoot   = "zsys-meta:modified-boot"
	noModifiedBoot = "zsys-meta:no-modified-boot"
)

func init() {
	bootCmd.PersistentFlags().BoolVarP(&printModifiedBoot, "print-changes", "p", false, "Display if any zfs datasets have been modified to boot")
	rootCmd.AddCommand(bootCmd)
	bootCmd.AddCommand(bootPrepareCmd)
	bootCmd.AddCommand(bootCommitCmd)
}

func bootPrepare(printModifiedBoot bool) (err error) {
	z := zfs.New(context.Background(), zfs.WithTransactions())

	defer func() {
		if err != nil {
			z.Cancel()
			err = fmt.Errorf("couldn't ensure boot: "+config.ErrorFormat, err)
		} else {
			z.Done()
		}
	}()

	ms, err := getMachines(context.Background(), z)
	if err != nil {
		return err
	}

	changed, err := ms.EnsureBoot(context.Background(), z)
	if err != nil {
		return err
	}
	if printModifiedBoot && changed {
		fmt.Println(modifiedBoot)
	} else if printModifiedBoot && !changed {
		fmt.Println(noModifiedBoot)
	}

	return nil
}

func bootCommit(printModifiedBoot bool) (err error) {
	client, err := newClient()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(client.Ctx, zsys.DefaultTimeout)
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
		fmt.Println(modifiedBoot)
	} else if printModifiedBoot && !changed {
		fmt.Println(noModifiedBoot)
	}

	return nil
}
