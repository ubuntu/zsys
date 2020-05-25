package client

import (
	"io"

	"github.com/spf13/cobra"
	"github.com/ubuntu/zsys"
	"github.com/ubuntu/zsys/cmd/zsysd/cmdhandler"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/streamlogger"
)

var (
	userdataCmd = &cobra.Command{
		Use:    "userdata COMMAND",
		Short:  i18n.G("User datasets creation and rename"),
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
	userdataDissociateCmd = &cobra.Command{
		Use:   "dissociate USER",
		Short: i18n.G("dissociate current user data from current system but preserve history"),
		Args:  cobra.ExactArgs(1),
		Run:   func(cmd *cobra.Command, args []string) { cmdErr = dissociateUser(args[0]) },
	}
)

func init() {
	rootCmd.AddCommand(userdataCmd)
	userdataCmd.AddCommand(userdataCreateCmd)
	userdataCmd.AddCommand(userdataRenameCmd)
	userdataCmd.AddCommand(userdataDissociateCmd)
}

// createUserData creates a new userdata for user and set it to homepath on current zsys system.
// if the user already exists for a dataset attached to the current system, set its mountpoint to homepath.
func createUserData(user, homepath string) (err error) {
	client, err := newClient()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel, reset := contextWithResettableTimeout(client.Ctx, config.DefaultClientTimeout)
	defer cancel()

	stream, err := client.CreateUserData(ctx, &zsys.CreateUserDataRequest{User: user, Homepath: homepath})
	if err = checkConn(err, reset); err != nil {
		return err
	}

	for {
		_, err := stream.Recv()
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

	ctx, cancel, reset := contextWithResettableTimeout(client.Ctx, config.DefaultClientTimeout)
	defer cancel()

	stream, err := client.ChangeHomeOnUserData(ctx, &zsys.ChangeHomeOnUserDataRequest{Home: home, NewHome: newHome})
	if err = checkConn(err, reset); err != nil {
		return err
	}

	for {
		_, err := stream.Recv()
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
	}

	return nil
}

// dissociateUser dissociates current user data from current system but preserve history.
func dissociateUser(user string) (err error) {
	client, err := newClient()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel, reset := contextWithResettableTimeout(client.Ctx, config.DefaultClientTimeout)
	defer cancel()

	stream, err := client.DissociateUser(ctx, &zsys.DissociateUserRequest{User: user})
	if err = checkConn(err, reset); err != nil {
		return err
	}

	for {
		_, err := stream.Recv()
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
	}

	return nil
}
