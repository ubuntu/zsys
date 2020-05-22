package zfs

import (
	"fmt"
	"strconv"

	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/zfs/libzfs"
)

// GetPoolFreeSpace returns the available free space on the pool
func (z Zfs) GetPoolFreeSpace(n string) (freespace int, err error) {
	p, err := z.libzfs.PoolOpen(n)
	if err != nil {
		return 0, fmt.Errorf(i18n.G("Couldn't open pool %s: %v"), n, err)
	}
	defer p.Close()
	cap := p.Properties[libzfs.PoolPropCapacity].Value
	freespace, err = strconv.Atoi(cap)
	if err != nil {
		return 0, fmt.Errorf(i18n.G("Invalid capacity %q on pool %q: %v"), cap, n, err)
	}
	return 100 - freespace, nil
}
