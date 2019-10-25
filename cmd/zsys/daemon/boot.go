package daemon

import (
	"context"
	"fmt"

	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/zfs"
)

func syncBootPrepare() error {
	z := zfs.New(context.Background(), zfs.WithTransactions())

	var err error
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

	if changed {
		fmt.Println(config.ModifiedBoot)
	} else {
		fmt.Println(config.NoModifiedBoot)
	}

	return nil
}
