package daemon

import (
	"context"
	"fmt"
	"io/ioutil"

	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/machines"
	"github.com/ubuntu/zsys/internal/zfs"
)

func syncBootPrepare() (err error) {
	z := zfs.NewWithAutoCancel(context.Background())
	defer z.DoneCheckErr(&err)

	ms, err := getMachines(context.Background(), z)
	if err != nil {
		return err
	}

	changed, err := ms.EnsureBoot(context.Background(), z)
	if err != nil {
		return fmt.Errorf("couldn't ensure boot: "+config.ErrorFormat, err)
	}

	if changed {
		fmt.Println(config.ModifiedBoot)
	} else {
		fmt.Println(config.NoModifiedBoot)
	}

	return nil
}

// getMachines returns all scanned machines on the current system
func getMachines(ctx context.Context, z *zfs.Zfs) (*machines.Machines, error) {
	ds, err := z.Scan()
	if err != nil {
		return nil, err
	}
	cmdline, err := procCmdline()
	if err != nil {
		return nil, err
	}
	ms := machines.New(ctx, ds, cmdline)

	return &ms, nil
}

// procCmdline returns kernel command line
func procCmdline() (string, error) {
	content, err := ioutil.ReadFile("/proc/cmdline")
	if err != nil {
		return "", err
	}

	return string(content), nil
}
