package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/ubuntu/zsys/cmd/zsysd/client"
	"github.com/ubuntu/zsys/cmd/zsysd/daemon"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/i18n"
)

//go:generate go run ./generate-mancomp.go cobracompletion.go completion.go completion ../../generated
//go:generate go run ./generate-mancomp.go cobracompletion.go completion.go man ../../generated
//go:generate go run ./generate-mancomp.go cobracompletion.go completion.go update-readme

func main() {
	i18n.InitI18nDomain(config.TEXTDOMAIN)

	var rootCmd *cobra.Command
	var errFunc func() error

	if filepath.Base(os.Args[0]) == "zsysd" {
		rootCmd = daemon.Cmd()
		errFunc = daemon.Error
	} else {
		rootCmd = client.Cmd()
		errFunc = client.Error
	}
	installCompletionCmd(rootCmd)

	if err := rootCmd.Execute(); err != nil {
		// This is a usage Error (we don't use postfix E commands other than usage)
		// Usage error should be the same format than other errors
		log.SetFormatter(&log.TextFormatter{
			DisableLevelTruncation: true,
			DisableTimestamp:       true,
		})
		log.Error(err)
		os.Exit(2)
	}
	err := errFunc()
	if err != nil {
		if errors.Is(err, context.Canceled) {
			err = errors.New(i18n.G("Service took too long to respond. Disconnecting client."))
		}
		log.Error(err)
		os.Exit(1)
	}
}
