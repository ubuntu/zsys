package daemon

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/log"
)

const (
	updateGrubCmd = "update-grub"
)

func updateBootMenu(ctx context.Context) error {
	cmd := exec.Command(updateGrubCmd)
	logger := &logWriter{ctx: ctx}
	cmd.Stdout = logger
	cmd.Stderr = logger
	if err := cmd.Run(); err != nil {
		return fmt.Errorf(i18n.G("%q returned an error: ")+config.ErrorFormat, updateGrubCmd, err)
	}
	return nil
}

type logWriter struct {
	ctx context.Context
}

func (lw logWriter) Write(p []byte) (n int, err error) {
	log.Debug(lw.ctx, string(p))
	return len(p), nil
}
