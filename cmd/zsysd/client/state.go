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
		Run:   func(cmd *cobra.Command, args []string) { cmdErr = saveState(args) },
	}
	stateremoveCmd = &cobra.Command{
		Use:   "remove [state id]",
		Short: i18n.G("Remove the current state of the machine. By default it removes only the user state if not linked to any system state."),
		Args:  cobra.MaximumNArgs(1),
		Run:   func(cmd *cobra.Command, args []string) { cmdErr = removeState(args) },
	}
)

var (
	system   bool
	userName string
	force    bool
)

func init() {
	rootCmd.AddCommand(stateCmd)
	stateCmd.AddCommand(statesaveCmd)
	stateCmd.AddCommand(stateremoveCmd)

	statesaveCmd.Flags().BoolVarP(&system, "system", "s", false, i18n.G("Save complete system state (users and system)"))
	statesaveCmd.Flags().StringVarP(&userName, "user", "u", "", i18n.G("Save the state for a given user or current user if empty"))

	// user name and system or exclusive: TODO
	stateremoveCmd.Flags().BoolVarP(&system, "system", "s", false, i18n.G("Remove system state (system and users linked to it)"))
	stateremoveCmd.Flags().StringVarP(&userName, "user", "u", "", i18n.G("Remove the state for a given user or current user if empty"))
	stateremoveCmd.Flags().BoolVarP(&force, "force", "f", false, i18n.G("Force removing, even if dependencies are found"))

	rootCmd.AddCommand(statesaveCmd) // Alias
}

func saveState(args []string) (err error) {

	if system && userName != "" {
		return errors.New(i18n.G("you can't provide system and user flags at the same time"))
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
		stream, err := client.SaveSystemState(ctx, &zsys.SaveSystemStateRequest{StateName: stateName})

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

	additionalRemovals, err := removeStateGRPC(client, force, system, userName, stateName)
	if err != nil {
		return err
	}

	// if no additional questions: we are successfully done
	if additionalRemovals == "" {
		return nil
	}

	fmt.Printf(i18n.G("%s\nWould you like to proceed [y/N]? "), additionalRemovals)
	reader := bufio.NewReader(os.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil {
		return err
	}
	answer = strings.ToLower(answer)

	if answer != i18n.G("y") || answer != i18n.G("yes") {
		return nil
	}

	_, err = removeStateGRPC(client, true, system, userName, stateName)

	return err
}

func removeStateGRPC(client *zsys.ZsysLogClient, force, system bool, userName, stateName string) (string, error) {
	var additionalRemovals string

	ctx, cancel := context.WithTimeout(client.Ctx, config.DefaultClientTimeout)
	defer cancel()

	if system {
		stream, err := client.RemoveSystemState(ctx, &zsys.RemoveSystemStateRequest{
			StateName: stateName,
			Force:     force,
		})

		if err = checkConn(err); err != nil {
			return "", err
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
				return "", err
			}

			additionalRemovals = r.GetAdditionalRemovals()
		}
	} else {
		stream, err := client.RemoveUserState(ctx, &zsys.RemoveUserStateRequest{
			StateName: stateName,
			UserName:  userName,
			Force:     force,
		})

		if err = checkConn(err); err != nil {
			return "", err
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
				return "", err
			}

			additionalRemovals = r.GetAdditionalRemovals()
		}
	}

	return additionalRemovals, nil
}
