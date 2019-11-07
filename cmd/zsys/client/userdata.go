package client

import (
	"context"
	"io"

	"github.com/spf13/cobra"
	"github.com/ubuntu/zsys"
	"github.com/ubuntu/zsys/cmd/zsys/cmdhandler"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/streamlogger"
)

var (
	userdataCmd = &cobra.Command{
		Use:    "userdata COMMAND",
		Short:  i18n.G("User datasets creation and renaming"),
		Hidden: true,
		Args:   cmdhandler.SubcommandsRequiredWithSuggestions,
		Run:    cmdhandler.NoCmd,
	}
	userdataCreateCmd = &cobra.Command{
		Use:   "create USER HOME_DIRECTORY",
		Short: i18n.G("Create a new home user dataset via an user dataset (if doesn't exist) creation"),
		Args:  cobra.ExactArgs(2),
		Run:   func(cmd *cobra.Command, args []string) { cmdErr = createUserData(args[0], args[1]) },
	}
	userdataRenameCmd = &cobra.Command{
		Use:   "set-home OLD_HOME NEW_HOME",
		Short: i18n.G("Rename a user's home directory via renaming corresponding user dataset"),
		Args:  cobra.ExactArgs(2),
		Run:   func(cmd *cobra.Command, args []string) { cmdErr = changeHomeOnUserData(args[0], args[1]) },
	}
)

func init() {
	rootCmd.AddCommand(userdataCmd)
	userdataCmd.AddCommand(userdataCreateCmd)
	userdataCmd.AddCommand(userdataRenameCmd)
}

// createUserData creates a new userdata for user and set it to homepath on current zsys system.
// if the user already exists for a dataset attached to the current system, set its mountpoint to homepath.
func createUserData(user, homepath string) (err error) {
	client, err := newClient()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(client.Ctx, config.DefaultClientTimeout)
	defer cancel()

	stream, err := client.CreateUserData(ctx, &zsys.CreateUserDataRequest{User: user, Homepath: homepath})
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

// changeHomeOnUserData change from home to newHome on current zsys system.
func changeHomeOnUserData(home, newHome string) (err error) {
	client, err := newClient()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(client.Ctx, config.DefaultClientTimeout)
	defer cancel()

	stream, err := client.ChangeHomeOnUserData(ctx, &zsys.ChangeHomeOnUserDataRequest{Home: home, NewHome: newHome})
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
