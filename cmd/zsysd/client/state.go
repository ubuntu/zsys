package client

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ubuntu/zsys"
	"github.com/ubuntu/zsys/cmd/zsysd/cmdhandler"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/streamlogger"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/status"
)

var (
	stateCmd = &cobra.Command{
		Use:   "state COMMAND",
		Short: i18n.G("Machine state management"),
		Args:  cmdhandler.SubcommandsRequiredWithSuggestions,
		Run:   cmdhandler.NoCmd,
	}
	statesaveCmd = &cobra.Command{
		Use:   "save [state id]",
		Short: i18n.G("Saves the current state of the machine. By default it saves only the user state. state_id is generated if not provided."),
		Args:  cobra.MaximumNArgs(1),
		Run:   func(cmd *cobra.Command, args []string) { cmdErr = saveState(args, system, userName, noUpdateBootMenu) },
	}
	stateremoveCmd = &cobra.Command{
		Use:   "remove [state id]",
		Short: i18n.G("Remove the current state of the machine. By default it removes only the user state if not linked to any system state."),
		Args:  cobra.MaximumNArgs(1),
		Run:   func(cmd *cobra.Command, args []string) { cmdErr = removeState(args) },
	}
)

var (
	system           bool
	noUpdateBootMenu bool
	userName         string
	force            bool
	dryrun           bool
)

func init() {
	rootCmd.AddCommand(stateCmd)
	stateCmd.AddCommand(statesaveCmd)
	stateCmd.AddCommand(stateremoveCmd)

	statesaveCmd.Flags().BoolVarP(&system, "system", "s", false, i18n.G("Save complete system state (users and system)"))
	statesaveCmd.Flags().StringVarP(&userName, "user", "u", "", i18n.G("Save the state for a given user or current user if empty"))
	statesaveCmd.Flags().BoolVarP(&noUpdateBootMenu, "no-update-bootmenu", "", false, i18n.G("Do not update bootmenu on system state save"))

	// user name and system or exclusive: TODO
	stateremoveCmd.Flags().BoolVarP(&system, "system", "s", false, i18n.G("Remove system state (system and users linked to it)"))
	stateremoveCmd.Flags().StringVarP(&userName, "user", "u", "", i18n.G("Remove the state for a given user or current user if empty"))
	stateremoveCmd.Flags().BoolVarP(&force, "force", "f", false, i18n.G("Force removing, even if dependencies are found"))
	stateremoveCmd.Flags().BoolVarP(&dryrun, "dryrun", "", false, i18n.G("Dry run, will not remove anything"))

	cmdhandler.RegisterAlias(statesaveCmd, rootCmd)
}

func saveState(args []string, system bool, userName string, noUpdateBootMenu bool) (err error) {

	if system && userName != "" {
		return errors.New(i18n.G("you can't provide system and user flags at the same time"))
	}
	if !system && noUpdateBootMenu {
		return errors.New(i18n.G("you can't provide no-update-bootmenu option on user state save"))
	}

	var stateName string
	if len(args) > 0 {
		stateName = args[0]
	}

	client, err := newClient()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(client.Ctx, config.DefaultClientTimeout)
	defer cancel()

	if system {
		stream, err := client.SaveSystemState(ctx, &zsys.SaveSystemStateRequest{
			StateName:      stateName,
			UpdateBootMenu: !noUpdateBootMenu,
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

			stateName = r.GetStateName()
		}
	} else {
		if userName == "" {
			user, err := user.Current()
			if err != nil {
				return fmt.Errorf("Couldn’t determine current user name: %v", err)
			}
			userName = user.Username
		}

		stream, err := client.SaveUserState(ctx, &zsys.SaveUserStateRequest{UserName: userName, StateName: stateName})

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

			stateName = r.GetStateName()
		}
	}

	fmt.Printf(i18n.G("Successfully saved as %q\n"), stateName)

	return nil
}

func removeState(args []string) (err error) {
	if system && userName != "" {
		return errors.New(i18n.G("you can't provide system and user flags at the same time"))
	}

	if len(args) != 1 {
		return errors.New(i18n.G("one and only one state to delete should be provided"))
	}

	stateName := args[0]

	// prefill with current user
	if !system && userName == "" {
		user, err := user.Current()
		if err != nil {
			return fmt.Errorf("Couldn’t determine current user name: %v", err)
		}
		userName = user.Username
	}

	client, err := newClient()
	if err != nil {
		return err
	}
	defer client.Close()

	for {
		err = removeStateGRPC(client, force, dryrun, system, userName, stateName)
		if err == nil {
			break
		}

		var recoverableMsg string
		s := status.Convert(err)
		for _, d := range s.Details() {
			switch info := d.(type) {
			case *errdetails.ErrorInfo:
				// Does the request needs confirmation?
				if info.Type == config.UserConfirmationNeeded {
					recoverableMsg = info.Metadata["msg"]
				}
			default:
			}
		}
		if recoverableMsg == "" {
			return err
		}

		fmt.Printf(i18n.G("%s\nWould you like to proceed [y/N]? "), recoverableMsg)
		reader := bufio.NewReader(os.Stdin)
		answer, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		answer = strings.TrimSpace(strings.ToLower(answer))

		if !(answer == i18n.G("y") || answer == i18n.G("yes")) {
			break
		}
		force = true
	}

	return nil
}

func removeStateGRPC(client *zsys.ZsysLogClient, force, dryrun, system bool, userName, stateName string) error {
	ctx, cancel := context.WithTimeout(client.Ctx, config.DefaultClientTimeout)
	defer cancel()

	var err error
	if system {
		var stream zsys.Zsys_RemoveSystemStateClient
		stream, err = client.RemoveSystemState(ctx, &zsys.RemoveSystemStateRequest{
			StateName: stateName,
			Force:     force,
			Dryrun:    dryrun,
		})

		if err = checkConn(err); err != nil {
			return err
		}

		for {
			_, err = stream.Recv()
			if err == streamlogger.ErrLogMsg {
				continue
			}
			if err != nil {
				if err == io.EOF {
					err = nil
				}
				break
			}
		}
	} else {
		var stream zsys.Zsys_RemoveUserStateClient
		stream, err = client.RemoveUserState(ctx, &zsys.RemoveUserStateRequest{
			StateName: stateName,
			UserName:  userName,
			Force:     force,
			Dryrun:    dryrun,
		})

		if err = checkConn(err); err != nil {
			return err
		}

		for {
			_, err = stream.Recv()
			if err == streamlogger.ErrLogMsg {
				continue
			}
			if err != nil {
				if err == io.EOF {
					err = nil
				}
				break
			}
		}
	}

	return err
}
