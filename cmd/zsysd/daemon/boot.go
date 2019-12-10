package daemon

import (
	"context"
	"fmt"
	"io/ioutil"

	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/machines"
)

func syncBootPrepare() (err error) {
	cmdline, err := procCmdline()
	if err != nil {
		return fmt.Errorf(i18n.G("couldn't parse kernel command line: %v"), err)
	}
	ms, err := machines.New(context.Background(), cmdline)
	if err != nil {
		return fmt.Errorf(i18n.G("couldn't create a new machine: %v"), err)
	}

	changed, err := ms.EnsureBoot(context.Background())
	if err != nil {
		return fmt.Errorf(i18n.G("couldn't ensure boot: ")+config.ErrorFormat, err)
	}

	if changed {
		fmt.Println(config.ModifiedBoot)
	} else {
		fmt.Println(config.NoModifiedBoot)
	}

	return nil
}

// procCmdline returns kernel command line
func procCmdline() (string, error) {
	content, err := ioutil.ReadFile("/proc/cmdline")
	if err != nil {
		return "", err
	}

	return string(content), nil
}
