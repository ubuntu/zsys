package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/user"

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
)

var (
	saveSystem bool
	userName   string
)

func init() {
	rootCmd.AddCommand(stateCmd)
	stateCmd.AddCommand(statesaveCmd)
	statesaveCmd.Flags().BoolVarP(&saveSystem, "system", "s", false, i18n.G("Save complete system state (users and system)"))
	statesaveCmd.Flags().StringVarP(&userName, "user", "u", "", i18n.G("Save the state for a given user or current user if empty"))

	rootCmd.AddCommand(statesaveCmd) // Alias
}

func saveState(args []string) (err error) {

	if saveSystem && userName != "" {
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

	if saveSystem {
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
