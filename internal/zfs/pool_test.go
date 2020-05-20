package zfs_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/ubuntu/zsys/internal/testutils"
	"github.com/ubuntu/zsys/internal/zfs"
	"github.com/ubuntu/zsys/internal/zfs/libzfs/mock"
)

func TestGetFreeSpace(t *testing.T) {
	failOnZFSPermissionDenied(t)

	tests := map[string]struct {
		pool     string
		capacity string

		wantFree int
		wantErr  bool
	}{
		"Free space returned": {pool: "rpool", capacity: "80", wantFree: 20},

		"Capacity is not a number":  {pool: "rpool", capacity: "NaN", wantErr: true},
		"Called on unexisting pool": {pool: "doesntexist", wantErr: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			dir, cleanup := testutils.TempDir(t)
			defer cleanup()

			adapter := testutils.GetLibZFS(t)

			lzfs, ok := adapter.(*mock.LibZFS)
			if !ok {
				t.Skip("Can only be called with the mock libzfs")
			}

			fPools := testutils.NewFakePools(t, filepath.Join("testdata", "one_pool_one_dataset.yaml"), testutils.WithLibZFS(adapter))
			defer fPools.Create(dir)()
			lzfs.SetPoolCapacity("rpool", tc.capacity)

			z, err := zfs.New(context.Background(), zfs.WithLibZFS(adapter))
			if err != nil {
				t.Fatalf("ZFS new errored out when we expected not to: %v", err)
			}

			free, err := z.GetPoolFreeSpace(tc.pool)
			if err != nil {
				if !tc.wantErr {
					t.Fatalf("expected no error but got: %v", err)
				}
				return
			}
			if tc.wantErr {
				t.Fatal("expected an error but got none")
			}

			assert.Equal(t, free, tc.wantFree, "Free capacity is the expected value")
		})
	}
}
