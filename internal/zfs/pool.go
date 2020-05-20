package zfs

import (
	"fmt"
	"strconv"

	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/zfs/libzfs"
)

// GetPoolCapacity returns the capacity of pool n
func (z Zfs) GetPoolFreeSpace(n string) (freespace int, err error) {
	p, err := z.libzfs.PoolOpen(n)
	if err != nil {
		return 0, fmt.Errorf(i18n.G("Couldn't open pool %s: %v"), n, err)
	}
	defer p.Close()
	prop, err := p.GetProperty(libzfs.PoolPropCapacity)
	if err != nil {
		return 0, fmt.Errorf(i18n.G("Couldn't get capacity on pool %s: %v"), n, err)
	}
	freespace, err = strconv.Atoi(prop.Value)
	if err != nil {
		return 0, fmt.Errorf(i18n.G("Invalid capacity %s on pool %s: %v"), prop.Value, n, err)
	}
	return 100 - freespace, nil
}
