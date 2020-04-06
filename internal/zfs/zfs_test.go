package zfs_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/k0kubun/pp"
	"github.com/stretchr/testify/assert"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/testutils"
	"github.com/ubuntu/zsys/internal/zfs"
	"github.com/ubuntu/zsys/internal/zfs/libzfs"
)

func init() {
	config.SetVerboseMode(2)
}

func TestNew(t *testing.T) {
	failOnZFSPermissionDenied(t)

	tests := map[string]struct {
		def     string
		mounted string

		setInvalidLastUsed string

		wantErr bool
	}{
		"One pool, N datasets, N children":                                         {def: "one_pool_n_datasets_n_children.yaml"},
		"One pool, N datasets, N children, N snapshots":                            {def: "one_pool_n_datasets_n_children_n_snapshots.yaml"},
		"One pool, N datasets, N children, N snapshots, intermediate canmount=off": {def: "one_pool_n_datasets_n_children_n_snapshots_canmount_off.yaml"},
		"One pool, one dataset":                                                    {def: "one_pool_one_dataset.yaml"},
		"One pool, one dataset, different mountpoints":                             {def: "one_pool_one_dataset_different_mountpoints.yaml"},
		"One pool, one dataset, no property":                                       {def: "one_pool_one_dataset_no_property.yaml"},
		"One pool, one dataset, with bootfsdatasets property":                      {def: "one_pool_one_dataset_with_bootfsdatasets.yaml"},
		"One pool, one dataset, with bootfsdatasets property, multiple elems":      {def: "one_pool_one_dataset_with_bootfsdatasets_multiple.yaml"},
		"One pool, one dataset, with lastused property":                            {def: "one_pool_one_dataset_with_lastused.yaml"},
		"One pool, one dataset, with lastbootedkernel property":                    {def: "one_pool_one_dataset_with_lastbootedkernel.yaml"},
		"One pool, with canmount as default":                                       {def: "one_pool_dataset_with_canmount_default.yaml"},
		"One pool, N datasets":                                                     {def: "one_pool_n_datasets.yaml"},
		"One pool, one dataset, one snapshot":                                      {def: "one_pool_one_dataset_one_snapshot.yaml"},
		"One pool, one dataset, canmount=noauto":                                   {def: "one_pool_one_dataset_canmount_noauto.yaml"},
		"One pool, N datasets, one snapshot":                                       {def: "one_pool_n_datasets_one_snapshot.yaml"},
		"One pool non-root mpoint, N datasets no mountpoint":                       {def: "one_pool_with_nonroot_mountpoint_n_datasets_no_mountpoint.yaml"},
		"Two pools, N datasets":                                                    {def: "two_pools_n_datasets.yaml"},
		"Two pools, N datasets, N snapshots":                                       {def: "two_pools_n_datasets_n_snapshots.yaml"},
		"One mounted dataset":                                                      {def: "one_pool_n_datasets_n_children.yaml", mounted: "rpool/ROOT/ubuntu"},
		"Snapshot user properties differs from parent dataset":                     {def: "one_pool_one_dataset_one_snapshot_with_user_properties.yaml"},
		"Snapshot with unset user properties inherits from parent dataset":         {def: "one_pool_n_datasets_n_children_n_snapshots_with_unset_user_properties.yaml"},
		"Snapshot without any users properties are still loaded":                   {def: "one_pool_one_dataset_one_snapshot_without_user_properties.yaml"},
		"Snapshot ignores bootfsdataset user property even if set":                 {def: "one_pool_one_dataset_one_snapshot_with_bootfsdatasets.yaml"},
		"Layout with none, default properties and snapshot":                        {def: "layout1__one_pool_n_datasets_one_main_snapshots_inherited.yaml"},
		"One pool, one dataset with invalid lastUsed":                              {def: "one_pool_one_dataset.yaml", setInvalidLastUsed: "rpool"},
		"One pool, one dataset, one snapshot no source on user property":           {def: "one_pool_one_dataset_one_snapshot_no_source_on_userproperty.yaml"},

		"One pool, N datasets, ignore volume": {def: "one_pool_n_datasets_with_volume.yaml"},
		// TODO: add bookmark creation support to go-libzfs
		//"One pool, one dataset, one snapshot, ignore bookmark": {def: "one_pool_one_dataset_one_snapshot_with_bookmark.yaml"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			dir, cleanup := testutils.TempDir(t)
			defer cleanup()

			ta := timeAsserter(time.Now())
			adapter := testutils.GetLibZFS(t)
			fPools := testutils.NewFakePools(t, filepath.Join("testdata", tc.def), testutils.WithLibZFS(adapter))
			defer fPools.Create(dir)()

			if tc.mounted != "" {
				temp := filepath.Join(dir, "tempmount")
				if err := os.MkdirAll(temp, 0755); err != nil {
					t.Fatalf("couldn't create temporary mount point directory %q: %v", temp, err)
				}
				if testutils.UseSystemZFS() {
					// zfs will unmount it when exporting the pool
					if err := syscall.Mount(tc.mounted, temp, "zfs", 0, "zfsutil"); err != nil {
						t.Fatalf("couldn't prepare and mount %q: %v", tc.mounted, err)
					}
				} else {

					d, err := adapter.DatasetOpen(tc.mounted)
					if err != nil {
						t.Fatalf("couldn't open dataset %q", tc.mounted)
					}
					if err := d.SetProperty(libzfs.DatasetPropMounted, "yes"); err != nil {
						t.Fatalf("couldn't set mounted attribute on %q", tc.mounted)
					}
				}

			}

			if tc.setInvalidLastUsed != "" {
				d, err := adapter.DatasetOpen(tc.setInvalidLastUsed)
				if err != nil {
					t.Fatalf("couldn't open %q to set invalid LastUsed: %v", tc.setInvalidLastUsed, err)
				}
				if err := d.SetUserProperty(libzfs.LastUsedProp, "invalid"); err != nil {
					t.Fatalf("couldn't set invalid LastUsed on %q: %v", tc.setInvalidLastUsed, err)
				}
			}

			z, err := zfs.New(context.Background(), zfs.WithLibZFS(adapter))
			if err != nil {
				if !tc.wantErr {
					t.Fatalf("expected no error but got: %v", err)
				}
				return
			}
			if tc.wantErr {
				t.Fatal("expected an error but got none")
			}

			zfs.AssertNoZFSChildren(t, z)
			assertDatasetsToGolden(t, ta, z.Datasets())
		})
	}
}

func TestRefresh(t *testing.T) {
	failOnZFSPermissionDenied(t)
	dir, cleanup := testutils.TempDir(t)
	defer cleanup()

	ta := timeAsserter(time.Now())
	adapter := testutils.GetLibZFS(t)
	fPools := testutils.NewFakePools(t, filepath.Join("testdata", "one_pool_one_dataset.yaml"), testutils.WithLibZFS(adapter))
	defer fPools.Create(dir)()

	z, err := zfs.New(context.Background(), zfs.WithLibZFS(adapter))
	if err != nil {
		t.Fatalf("expected no error but got: %v", err)
	}

	oldZ := *z
	if err := z.Refresh(context.Background()); err != nil {
		t.Fatalf("expected no error but got: %v", err)
	}

	assertDatasetsEquals(t, ta, oldZ.Datasets(), z.Datasets())
}

func TestCreate(t *testing.T) {
	failOnZFSPermissionDenied(t)

	tests := map[string]struct {
		def        string
		path       string
		mountpoint string
		canmount   string

		wantErr bool
	}{
		"Simple creation":                    {def: "one_pool_one_dataset.yaml", path: "rpool/dataset", mountpoint: "/home/foo", canmount: "on"},
		"Without mountpoint":                 {def: "one_pool_one_dataset.yaml", path: "rpool/dataset", canmount: "on"},
		"With canmount false":                {def: "one_pool_one_dataset.yaml", path: "rpool/dataset", mountpoint: "/home/foo", canmount: "off"},
		"With mountpoint and canmount false": {def: "one_pool_one_dataset.yaml", path: "rpool/dataset", canmount: "off"},

		"Failing on dataset already exists":        {def: "one_pool_n_datasets.yaml", path: "rpool/ROOT/ubuntu", wantErr: true},
		"Failing on pool directly":                 {def: "one_pool_one_dataset.yaml", path: "rpool", wantErr: true},
		"Failing on unexisting pool":               {def: "one_pool_one_dataset.yaml", path: "rpool2", wantErr: true},
		"Failing on missing intermediate datasets": {def: "one_pool_one_dataset.yaml", path: "rpool/intermediate/doesnt/exist", wantErr: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			dir, cleanup := testutils.TempDir(t)
			defer cleanup()

			ta := timeAsserter(time.Now())
			adapter := testutils.GetLibZFS(t)
			fPools := testutils.NewFakePools(t, filepath.Join("testdata", tc.def), testutils.WithLibZFS(adapter))
			defer fPools.Create(dir)()
			z, err := zfs.New(context.Background(), zfs.WithLibZFS(adapter))
			if err != nil {
				t.Fatalf("expected no error but got: %v", err)
			}
			initState := copyState(z)
			trans, _ := z.NewTransaction(context.Background())
			defer trans.Done()

			err = trans.Create(tc.path, tc.mountpoint, tc.canmount)

			if err != nil && !tc.wantErr {
				t.Fatalf("expected no error but got: %v", err)
			} else if err == nil && tc.wantErr {
				t.Fatal("expected an error but got none")
			}

			// check we didn't change anything on error
			if err != nil {
				assertDatasetsEquals(t, ta, initState, z.Datasets())
			} else {
				assertDatasetsToGolden(t, ta, z.Datasets())
			}

			zfs.AssertNoZFSChildren(t, z)
			assertIdempotentWithNew(t, ta, z.Datasets(), adapter)
		})
	}
}

func TestSnapshot(t *testing.T) {
	failOnZFSPermissionDenied(t)

	tests := map[string]struct {
		def          string
		snapshotName string
		datasetName  string
		recursive    bool

		wantErr bool
		isNoOp  bool
	}{
		"Simple snapshot":                                      {def: "one_pool_one_dataset.yaml", snapshotName: "snap1", datasetName: "rpool"},
		"Simple snapshot with children":                        {def: "layout1__one_pool_n_datasets.yaml", snapshotName: "snap1", datasetName: "rpool/ROOT/ubuntu_1234"},
		"Simple snapshot even if on subdataset already exists": {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", snapshotName: "snap_r1", datasetName: "rpool/ROOT"},

		"Recursive snapshots":                         {def: "layout1__one_pool_n_datasets.yaml", snapshotName: "snap1", datasetName: "rpool/ROOT/ubuntu_1234", recursive: true},
		"Recursive snapshot on leaf dataset":          {def: "one_pool_one_dataset.yaml", snapshotName: "snap1", datasetName: "rpool", recursive: true},
		"Recursive snapshots alongside existing ones": {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", snapshotName: "snap1", datasetName: "rpool/ROOT/ubuntu_1234", recursive: true},

		"Dataset doesn't exist":                             {def: "one_pool_one_dataset.yaml", snapshotName: "snap1", datasetName: "doesntexit", wantErr: true, isNoOp: true},
		"Invalid snapshot name":                             {def: "one_pool_one_dataset.yaml", snapshotName: "", datasetName: "rpool", wantErr: true, isNoOp: true},
		"Snapshot on dataset already exists":                {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", snapshotName: "snap_r1", datasetName: "rpool/ROOT/ubuntu_1234/opt", wantErr: true, isNoOp: true},
		"Snapshot on subdataset already exists":             {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", snapshotName: "snap_r1", datasetName: "rpool/ROOT", recursive: true, wantErr: true, isNoOp: true},
		"Snapshot on dataset exists, but not on subdataset": {def: "layout1_missing_intermediate_snapshot.yaml", snapshotName: "snap_r1", datasetName: "rpool/ROOT/ubuntu_1234", wantErr: true, isNoOp: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			dir, cleanup := testutils.TempDir(t)
			defer cleanup()

			ta := timeAsserter(time.Now())
			adapter := testutils.GetLibZFS(t)
			fPools := testutils.NewFakePools(t, filepath.Join("testdata", tc.def), testutils.WithLibZFS(adapter))
			defer fPools.Create(dir)()
			z, err := zfs.New(context.Background(), zfs.WithLibZFS(adapter))
			if err != nil {
				t.Fatalf("expected no error but got: %v", err)
			}
			initState := copyState(z)
			trans, _ := z.NewTransaction(context.Background())
			defer trans.Done()

			err = trans.Snapshot(tc.snapshotName, tc.datasetName, tc.recursive)

			if err != nil && !tc.wantErr {
				t.Fatalf("expected no error but got: %v", err)
			} else if err == nil && tc.wantErr {
				t.Fatal("expected an error but got none")
			}

			// check we didn't change anything on error
			if tc.isNoOp {
				assertDatasetsEquals(t, ta, initState, z.Datasets())
			}

			if err == nil && !tc.isNoOp {
				assertDatasetsToGolden(t, ta, z.Datasets())
			}

			zfs.AssertNoZFSChildren(t, z)
			assertIdempotentWithNew(t, ta, z.Datasets(), adapter)
		})
	}
}

func TestClone(t *testing.T) {
	failOnZFSPermissionDenied(t)

	tests := map[string]struct {
		def         string
		dataset     string
		suffix      string
		allowExists bool
		recursive   bool

		wantErr bool
		isNoOp  bool
	}{
		"Simple clone":                                       {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_1234@snap_r1", suffix: "5678"},
		"Recursive clone":                                    {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_1234@snap_r1", suffix: "5678", recursive: true},
		"Recursive clone on non root dataset":                {def: "layout1__one_pool_n_datasets_n_snapshots_with_started_clone.yaml", dataset: "rpool/ROOT/ubuntu_1234/var@snap_r1", suffix: "5678", recursive: true},
		"Recursive clone on root dataset ending with slash":  {def: "layout1__one_pool_n_datasets_n_snapshots_root_ends_with_slash.yaml", dataset: "rpool/ROOT/ubuntu_@snap_r1", suffix: "5678", recursive: true},
		"Simple clone ignore missing intermediate snapshots": {def: "layout1_missing_intermediate_snapshot.yaml", dataset: "rpool/ROOT/ubuntu_1234@snap_r1", suffix: "5678"},

		"Simple clone keeps canmount off as off":               {def: "one_pool_n_datasets_one_snapshot_with_canmount_off.yaml", dataset: "rpool/ROOT/ubuntu@snap1", suffix: "5678"},
		"Simple clone keeps canmount noauto as noauto":         {def: "one_pool_n_datasets_one_snapshot_with_canmount_noauto.yaml", dataset: "rpool/ROOT/ubuntu@snap1", suffix: "5678"},
		"Simple clone set canmount on to noauto":               {def: "one_pool_n_datasets_one_snapshot.yaml", dataset: "rpool/ROOT/ubuntu@snap1", suffix: "5678"},
		"Simple clone ignore canmount set to noauto (default)": {def: "one_pool_n_datasets_one_snapshot_with_canmount_noauto_default.yaml", dataset: "rpool/ROOT/ubuntu@snap1", suffix: "5678"},

		"Simple clone on non root local mountpoint keeps mountpoints": {def: "one_pool_n_datasets_one_snapshot_non_root.yaml", dataset: "rpool/ROOT/ubuntu@snap1", suffix: "5678"},

		"Simple clone set bootfs (local)":                {def: "one_pool_n_datasets_n_snapshots_with_bootfs_n_sources.yaml", dataset: "rpool/ROOT/ubuntu@snap_local", suffix: "5678"},
		"Recursive clone set bootfs ignored (inherited)": {def: "one_pool_n_datasets_n_snapshots_with_bootfs_n_sources.yaml", dataset: "rpool/ROOT/ubuntu@snap_inherited", suffix: "5678", recursive: true},
		"Simple clone set bootfs ignored (default)":      {def: "one_pool_n_datasets_n_snapshots_with_bootfs_n_sources.yaml", dataset: "rpool/ROOT/ubuntu@snap_default", suffix: "5678"},

		"Simple clone set lastbootedkernel (local)":                {def: "one_pool_n_datasets_n_snapshots_with_lastbootedkernel_n_sources.yaml", dataset: "rpool/ROOT/ubuntu@snap_local", suffix: "5678"},
		"Recursive clone set lastbootedkernel ignored (inherited)": {def: "one_pool_n_datasets_n_snapshots_with_lastbootedkernel_n_sources.yaml", dataset: "rpool/ROOT/ubuntu@snap_inherited", suffix: "5678", recursive: true},
		"Simple clone set lastbootedkernel ignored (default)":      {def: "one_pool_n_datasets_n_snapshots_with_lastbootedkernel_n_sources.yaml", dataset: "rpool/ROOT/ubuntu@snap_default", suffix: "5678"},

		"Recursive clone bootfsdataset ignored (local and inherited)": {def: "one_pool_n_datasets_n_snapshots_with_bootfsdataset_n_sources.yaml", dataset: "rpool/ROOT/ubuntu@snap_local_inherited", suffix: "5678", recursive: true},
		"Simple clone bootfsdataset ignored (default)":                {def: "one_pool_n_datasets_n_snapshots_with_bootfsdataset_n_sources.yaml", dataset: "rpool/ROOT/ubuntu2@snap_default", suffix: "5678"},

		"Recursive clone lastused ignored (local and inherited)": {def: "one_pool_n_datasets_n_snapshots_with_lastused_n_sources.yaml", dataset: "rpool/ROOT/ubuntu@snap_local_inherited", suffix: "5678", recursive: true},
		"Simple clone lastused ignored (default)":                {def: "one_pool_n_datasets_n_snapshots_with_lastused_n_sources.yaml", dataset: "rpool/ROOT/ubuntu2@snap_default", suffix: "5678"},

		"Simple clone on dataset without suffix":    {def: "layout1__one_pool_n_datasets_n_snapshots_without_suffix.yaml", dataset: "rpool/ROOT/ubuntu@snap_r1", suffix: "5678"},
		"Recursive clone on dataset without suffix": {def: "layout1__one_pool_n_datasets_n_snapshots_without_suffix.yaml", dataset: "rpool/ROOT/ubuntu@snap_r1", suffix: "5678", recursive: true},

		"Recursive missing some leaf snapshots":    {def: "layout1_missing_leaf_snapshot.yaml", dataset: "rpool/ROOT/ubuntu_1234@snap_r1", suffix: "5678", recursive: true},
		"Recursive missing intermediate snapshots": {def: "layout1_missing_intermediate_snapshot.yaml", dataset: "rpool/ROOT/ubuntu_1234@snap_r1", suffix: "5678", recursive: true, wantErr: true, isNoOp: true},

		"Existing datasets are skipped when requested": {def: "layout1_with_bootfs_already_cloned.yaml", dataset: "rpool/ROOT/ubuntu_1234@snap_r1", suffix: "5678", allowExists: true, recursive: true},

		"Snapshot doesn't exists":         {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_1234@doesntexists", suffix: "5678", wantErr: true, isNoOp: true},
		"Dataset doesn't exists":          {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_doesntexist@something", suffix: "5678", wantErr: true, isNoOp: true},
		"Not a snapshot":                  {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_1234", suffix: "5678", wantErr: true, isNoOp: true},
		"No suffix provided":              {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_1234@snap_r1", wantErr: true, isNoOp: true},
		"Suffixed dataset already exists": {def: "layout1_with_bootfs_already_cloned.yaml", dataset: "rpool/ROOT/ubuntu_1234@snap_r1", suffix: "5678", wantErr: true, isNoOp: true},
		"Clone on root fails":             {def: "one_pool_one_dataset_one_snapshot.yaml", dataset: "rpool@snap1", suffix: "5678", wantErr: true, isNoOp: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			dir, cleanup := testutils.TempDir(t)
			defer cleanup()

			ta := timeAsserter(time.Now())
			adapter := testutils.GetLibZFS(t)
			fPools := testutils.NewFakePools(t, filepath.Join("testdata", tc.def), testutils.WithLibZFS(adapter))
			defer fPools.Create(dir)()
			z, err := zfs.New(context.Background(), zfs.WithLibZFS(adapter))
			if err != nil {
				t.Fatalf("expected no error but got: %v", err)
			}
			initState := copyState(z)
			trans, _ := z.NewTransaction(context.Background())
			defer trans.Done()

			err = trans.Clone(tc.dataset, tc.suffix, tc.allowExists, tc.recursive)

			if err != nil && !tc.wantErr {
				t.Fatalf("expected no error but got: %v", err)
			} else if err == nil && tc.wantErr {
				t.Fatal("expected an error but got none")
			}

			if tc.isNoOp {
				assertDatasetsEquals(t, ta, initState, z.Datasets())
				return
			}
			if err == nil && !tc.isNoOp {
				assertDatasetsToGolden(t, ta, z.Datasets())
			}

			zfs.AssertNoZFSChildren(t, z)
			assertIdempotentWithNew(t, ta, z.Datasets(), adapter)
		})
	}
}

func TestPromote(t *testing.T) {
	failOnZFSPermissionDenied(t)

	tests := map[string]struct {
		def     string
		dataset string

		// prepare cloning/promotion scenarios
		cloneFrom       string
		cloneOnlyOne    bool   // only clone root element to have misssing intermediate snapshots
		alreadyPromoted string // pre-promote a dataset and its children

		wantErr bool
		isNoOp  bool
	}{
		"Promote with snapshots on origin":              {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_5678", cloneFrom: "rpool/ROOT/ubuntu_1234@snap_r1"},
		"Promote missing some leaf snapshots":           {def: "layout1_missing_leaf_snapshot.yaml", dataset: "rpool/ROOT/ubuntu_5678", cloneFrom: "rpool/ROOT/ubuntu_1234@snap_r1"},
		"Promote with snapshots and ancestor snapshots": {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_5678", cloneFrom: "rpool/ROOT/ubuntu_1234@snap_r2"},

		"Promote already promoted hierarchy":  {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_1234", isNoOp: true},
		"Root of hierarchy already promoted":  {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_5678", cloneFrom: "rpool/ROOT/ubuntu_1234@snap_r1", alreadyPromoted: "rpool/ROOT/ubuntu_5678"},
		"Child of hierarchy already promoted": {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_5678", cloneFrom: "rpool/ROOT/ubuntu_1234@snap_r1", alreadyPromoted: "rpool/ROOT/ubuntu_5678/var"},

		"Dataset doesn't exists":                            {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_doesntexist", wantErr: true, isNoOp: true},
		"Promote a snapshot fails":                          {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_1234@snap_r1", wantErr: true, isNoOp: true},
		"Can't promote when missing intermediate snapshots": {def: "layout1_missing_intermediate_snapshot.yaml", dataset: "rpool/ROOT/ubuntu_5678", cloneFrom: "rpool/ROOT/ubuntu_1234@snap_r1", cloneOnlyOne: true, wantErr: true, isNoOp: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			dir, cleanup := testutils.TempDir(t)
			defer cleanup()

			ta := timeAsserter(time.Now())
			adapter := testutils.GetLibZFS(t)
			fPools := testutils.NewFakePools(t, filepath.Join("testdata", tc.def), testutils.WithWaitBetweenSnapshots(), testutils.WithLibZFS(adapter))
			defer fPools.Create(dir)()
			z, err := zfs.New(context.Background(), zfs.WithLibZFS(adapter))
			if err != nil {
				t.Fatalf("expected no error but got: %v", err)
			}

			// Scan initial state for no-op
			trans, _ := z.NewTransaction(context.Background())
			defer trans.Done()

			if tc.cloneFrom != "" {
				err := trans.Clone(tc.cloneFrom, "5678", false, !tc.cloneOnlyOne)
				if err != nil {
					t.Fatalf("couldn't setup testbed when cloning: %v", err)
				}
			}
			if tc.alreadyPromoted != "" {
				err := trans.Promote(tc.alreadyPromoted)
				if err != nil {
					t.Fatalf("couldn't setup testbed when prepromoting %q: %v", tc.alreadyPromoted, err)
				}
			}
			initState := copyState(z)

			err = trans.Promote(tc.dataset)

			if err != nil && !tc.wantErr {
				t.Fatalf("expected no error but got: %v", err)
			} else if err == nil && tc.wantErr {
				t.Fatal("expected an error but got none")
			}

			if tc.isNoOp {
				assertDatasetsEquals(t, ta, initState, z.Datasets())
				return
			}
			if err == nil && !tc.isNoOp {
				assertDatasetsToGolden(t, ta, z.Datasets())
			}

			zfs.AssertNoZFSChildren(t, z)
			assertIdempotentWithNew(t, ta, z.Datasets(), adapter)
		})
	}
}

func TestPromoteCloneTree(t *testing.T) {
	failOnZFSPermissionDenied(t)
	dir, cleanup := testutils.TempDir(t)
	defer cleanup()

	ta := timeAsserter(time.Now())
	adapter := testutils.GetLibZFS(t)
	fPools := testutils.NewFakePools(t, filepath.Join("testdata", "one_pool_n_clones.yaml"), testutils.WithWaitBetweenSnapshots(), testutils.WithLibZFS(adapter))
	defer fPools.Create(dir)()
	z, err := zfs.New(context.Background(), zfs.WithLibZFS(adapter))
	if err != nil {
		t.Fatalf("expected no error but got: %v", err)
	}

	// Scan initial state for no-op
	trans, _ := z.NewTransaction(context.Background())
	defer trans.Done()

	err = trans.Clone("rpool/ROOT/ubuntu_1234@snap1", "5678", false, false)
	assert.NoError(t, err, "error in setup")
	time.Sleep(time.Second)
	trans.Snapshot("snap2", "rpool/ROOT/ubuntu_5678", false)
	assert.NoError(t, err, "error in setup")
	time.Sleep(time.Second)
	trans.Snapshot("snap8888", "rpool/ROOT/ubuntu_5678", false)
	assert.NoError(t, err, "error in setup")
	err = trans.Clone("rpool/ROOT/ubuntu_5678@snap2", "9876", false, false)
	assert.NoError(t, err, "error in setup")
	time.Sleep(time.Second)
	trans.Snapshot("snap3", "rpool/ROOT/ubuntu_9876", false)
	assert.NoError(t, err, "error in setup")
	err = trans.Clone("rpool/ROOT/ubuntu_9876@snap3", "9999", false, false)
	assert.NoError(t, err, "error in setup")
	err = trans.Clone("rpool/ROOT/ubuntu_5678@snap8888", "8888", false, false)
	assert.NoError(t, err, "error in setup")

	err = trans.Promote("rpool/ROOT/ubuntu_9876")
	if err != nil {
		t.Fatalf("promoting 9876 failed: %v", err)
	}
	for _, d := range z.Datasets() {
		if d.IsSnapshot || d.Name == "rpool" || d.Name == "rpool/ROOT" {
			continue
		}

		if d.Name == "rpool/ROOT/ubuntu_9876" {
			assert.Equalf(t, "", d.Origin, "Origin of %s should be empty and is set to %s", d.Name, d.Origin)
		} else {
			assert.NotEqualf(t, "", d.Origin, "Origin of %s is empty when it shouldn't", d.Name)
		}
	}

	zfs.AssertNoZFSChildren(t, z)
	assertIdempotentWithNew(t, ta, z.Datasets(), adapter)

	err = trans.Promote("rpool/ROOT/ubuntu_8888")
	if err != nil {
		t.Fatalf("promoting 8888 failed: %v", err)
	}
	for _, d := range z.Datasets() {
		if d.IsSnapshot || d.Name == "rpool" || d.Name == "rpool/ROOT" {
			continue
		}

		if d.Name == "rpool/ROOT/ubuntu_8888" {
			assert.Equalf(t, "", d.Origin, "Origin of %s should be empty and is set to %s", d.Name, d.Origin)
		} else {
			assert.NotEqualf(t, "", d.Origin, "Origin of %s is empty when it shouldn't", d.Name)
		}
	}

	zfs.AssertNoZFSChildren(t, z)
	assertIdempotentWithNew(t, ta, z.Datasets(), adapter)
}

func TestDestroy(t *testing.T) {
	failOnZFSPermissionDenied(t)

	tests := map[string]struct {
		def     string
		dataset string

		// prepare cloning/promotion scenarios
		cloneFrom       string
		alreadyPromoted string // pre-promote a dataset and its children

		wantErr bool
		isNoOp  bool
	}{
		"Leaf simple":                    {def: "layout1__one_pool_n_datasets.yaml", dataset: "rpool/ROOT/ubuntu_1234/var/lib/apt"},
		"Hierarchy simple":               {def: "layout1__one_pool_n_datasets.yaml", dataset: "rpool/ROOT/ubuntu_1234"},
		"Hierarchy with promoted clones": {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_1234", cloneFrom: "rpool/ROOT/ubuntu_1234@snap_r2", alreadyPromoted: "rpool/ROOT/ubuntu_5678"},

		"Leaf snapshot simple": {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_1234/var/lib/apt@snap_r1"},
		"Hierarchy snapshot":   {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_1234@snap_r1"},

		"Dataset doesn't exists":                      {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_doesntexist", wantErr: true, isNoOp: true},
		"Hierarchy with unpromoted clones":            {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_1234", cloneFrom: "rpool/ROOT/ubuntu_1234@snap_r1", wantErr: true, isNoOp: true},
		"Hierarchy with unpromoted clones non root":   {def: "layout1__one_pool_n_datasets_n_snapshots_with_started_clone.yaml", dataset: "rpool/ROOT/ubuntu_1234", cloneFrom: "rpool/ROOT/ubuntu_1234/var@snap_r1", wantErr: true, isNoOp: true},
		"Hierarchy with snapshots can’t be destroyed": {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_1234", wantErr: true, isNoOp: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			dir, cleanup := testutils.TempDir(t)
			defer cleanup()

			ta := timeAsserter(time.Now())
			adapter := testutils.GetLibZFS(t)
			fPools := testutils.NewFakePools(t, filepath.Join("testdata", tc.def), testutils.WithWaitBetweenSnapshots(), testutils.WithLibZFS(adapter))
			defer fPools.Create(dir)()
			z, err := zfs.New(context.Background(), zfs.WithLibZFS(adapter))
			if err != nil {
				t.Fatalf("expected no error but got: %v", err)
			}

			// Scan initial state for no-op
			trans, _ := z.NewTransaction(context.Background())
			defer trans.Done()

			if tc.cloneFrom != "" {
				err := trans.Clone(tc.cloneFrom, "5678", false, true)
				if err != nil {
					t.Fatalf("couldn't setup testbed when cloning: %v", err)
				}
			}
			if tc.alreadyPromoted != "" {
				err := trans.Promote(tc.alreadyPromoted)
				if err != nil {
					t.Fatalf("couldn't setup testbed when prepromoting %q: %v", tc.alreadyPromoted, err)
				}
			}
			initState := copyState(z)

			err = z.NewNoTransaction(context.Background()).Destroy(tc.dataset)

			if err != nil && !tc.wantErr {
				t.Fatalf("expected no error but got: %v", err)
			} else if err == nil && tc.wantErr {
				t.Fatal("expected an error but got none")
			}

			if tc.isNoOp {
				assertDatasetsEquals(t, ta, initState, z.Datasets())
				return
			}
			if err == nil && !tc.isNoOp {
				assertDatasetsToGolden(t, ta, z.Datasets())
			}

			zfs.AssertNoZFSChildren(t, z)
			assertIdempotentWithNew(t, ta, z.Datasets(), adapter)
		})
	}
}

func TestSetProperty(t *testing.T) {
	failOnZFSPermissionDenied(t)

	tests := map[string]struct {
		def           string
		propertyName  string
		propertyValue string
		dataset       string
		force         bool

		wantErr   bool
		wantPanic bool
		isNoOp    bool
	}{
		"User property (local)":         {def: "one_pool_one_dataset_with_bootfsdatasets.yaml", propertyName: libzfs.BootfsDatasetsProp, propertyValue: "SetProperty Value", dataset: "rpool"},
		"Authorized property (local)":   {def: "one_pool_one_dataset_with_bootfsdatasets.yaml", propertyName: libzfs.CanmountProp, propertyValue: "noauto", dataset: "rpool"},
		"Authorized property (default)": {def: "one_pool_dataset_with_canmount_default.yaml", propertyName: libzfs.CanmountProp, propertyValue: "noauto", dataset: "rpool/ubuntu"},
		"User property (none)":          {def: "one_pool_one_dataset_with_bootfsdatasets.yaml", propertyName: libzfs.LastBootedKernelProp, propertyValue: "SetProperty Value", dataset: "rpool"},
		// There is no authorized native properties that can be "none"

		// Canmount prop is already checked in authorized
		"Mountpoint property": {def: "one_pool_one_dataset_with_bootfsdatasets.yaml", propertyName: libzfs.MountPointProp, propertyValue: "/foo", dataset: "rpool"},

		"User property (inherit)":            {def: "one_pool_n_datasets_n_children_with_bootfsdatasets.yaml", propertyName: libzfs.BootfsDatasetsProp, propertyValue: "SetProperty Value", dataset: "rpool/ROOT/ubuntu/var"},
		"User property (inherit but forced)": {def: "one_pool_n_datasets_n_children_with_bootfsdatasets.yaml", propertyName: libzfs.BootfsDatasetsProp, propertyValue: "SetProperty Value", dataset: "rpool/ROOT/ubuntu/var", force: true},
		// There is no authorized properties that can be inherited

		// Property inheritance tests
		"Inherit on authorized property (local)":         {def: "layout1__one_pool_n_datasets_one_main_snapshots_inherited.yaml", propertyName: libzfs.MountPointProp, propertyValue: "/newroot", dataset: "rpool/ROOT/ubuntu_1234"},
		"Don't inherit on authorized property (default)": {def: "layout1__one_pool_n_datasets_one_main_snapshots_inherited.yaml", propertyName: libzfs.CanmountProp, propertyValue: "noauto", dataset: "rpool/ROOT/ubuntu_1234"},
		"Inherit on user property (local)":               {def: "layout1__one_pool_n_datasets_one_main_snapshots_inherited.yaml", propertyName: libzfs.BootfsDatasetsProp, propertyValue: "New value", dataset: "rpool/ROOT/ubuntu_1234"},
		"Inherit on user property (none)":                {def: "layout1__one_pool_n_datasets_one_main_snapshots_inherited.yaml", propertyName: libzfs.LastBootedKernelProp, propertyValue: "New value", dataset: "rpool/ROOT/ubuntu_1234"},

		"User property on snapshot (local)":                       {def: "one_pool_one_dataset_one_snapshot_with_user_properties.yaml", propertyName: libzfs.LastBootedKernelProp, propertyValue: "SetProperty Value", dataset: "rpool@snap1"},
		"User property on snapshot (none)":                        {def: "one_pool_one_dataset_one_snapshot_without_user_properties.yaml", propertyName: libzfs.LastBootedKernelProp, propertyValue: "SetProperty Value", dataset: "rpool@snap1"},
		"User property on snapshot (inherit)":                     {def: "one_pool_one_dataset_one_snapshot_with_user_properties.yaml", propertyName: libzfs.MountPointProp, propertyValue: "/home/a/path", dataset: "rpool@snap1"},
		"User property on snapshot (inherit but forced)":          {def: "one_pool_one_dataset_one_snapshot_with_user_properties.yaml", propertyName: libzfs.MountPointProp, propertyValue: "/home/a/path", dataset: "rpool@snap1", force: true},
		"SnapshotMountpointProp is MountPointProp":                {def: "one_pool_one_dataset_one_snapshot_with_user_properties.yaml", propertyName: libzfs.SnapshotMountpointProp, propertyValue: "/home/a/path", dataset: "rpool@snap1", force: true},
		"Let set on BootfsDatasetsProp but don't load it (local)": {def: "one_pool_one_dataset_one_snapshot_with_bootfsdatasets.yaml", propertyName: libzfs.BootfsDatasetsProp, propertyValue: "SetProperty Value", dataset: "rpool@snap1"},
		"Let set on BootfsDatasetsProp but don't load it (none)":  {def: "one_pool_one_dataset_one_snapshot_without_user_properties.yaml", propertyName: libzfs.BootfsDatasetsProp, propertyValue: "SetProperty Value", dataset: "rpool@snap1"},

		"LastUsed with children":            {def: "one_pool_one_dataset_one_snapshot_with_user_properties.yaml", propertyName: libzfs.LastUsedProp, propertyValue: "42", dataset: "rpool"},
		"LastUsed is not a number":          {def: "one_pool_one_dataset_one_snapshot_with_user_properties.yaml", propertyName: libzfs.LastUsedProp, propertyValue: "not a number", dataset: "rpool", wantErr: true, isNoOp: true},
		"LastUsed is inherited by children": {def: "one_pool_n_datasets_n_children.yaml", propertyName: libzfs.LastUsedProp, propertyValue: "42", dataset: "rpool/ROOT/ubuntu"},
		"LastUsed set empty":                {def: "one_pool_n_datasets_n_children.yaml", propertyName: libzfs.LastUsedProp, propertyValue: "", dataset: "rpool/ROOT/ubuntu"},

		"Unauthorized property":  {def: "one_pool_one_dataset.yaml", propertyName: "snapdir", propertyValue: "/setproperty/value", dataset: "rpool", wantPanic: true},
		"Dataset doesn't exists": {def: "one_pool_one_dataset.yaml", propertyName: libzfs.BootfsDatasetsProp, propertyValue: "SetProperty Value", dataset: "rpool10", wantErr: true, isNoOp: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			dir, cleanup := testutils.TempDir(t)
			defer cleanup()

			ta := timeAsserter(time.Now())
			adapter := testutils.GetLibZFS(t)
			fPools := testutils.NewFakePools(t, filepath.Join("testdata", tc.def), testutils.WithLibZFS(adapter))
			defer fPools.Create(dir)()
			z, err := zfs.New(context.Background(), zfs.WithLibZFS(adapter))
			if err != nil {
				t.Fatalf("expected no error but got: %v", err)
			}
			initState := copyState(z)
			trans, _ := z.NewTransaction(context.Background())
			defer trans.Done()

			if tc.wantPanic {
				assert.Panics(t, func() { trans.SetProperty(tc.propertyName, tc.propertyValue, tc.dataset, tc.force) }, "Panic was expected but didn't happen")
				return
			}

			err = trans.SetProperty(tc.propertyName, tc.propertyValue, tc.dataset, tc.force)

			if err != nil && !tc.wantErr {
				t.Fatalf("expected no error but got: %v", err)
			} else if err == nil && tc.wantErr {
				t.Fatal("expected an error but got none")
			}

			// check we didn't change anything on error
			if tc.isNoOp {
				assertDatasetsEquals(t, ta, initState, z.Datasets())
			}

			if err == nil && !tc.isNoOp {
				assertDatasetsToGolden(t, ta, z.Datasets())
			}

			zfs.AssertNoZFSChildren(t, z)
			assertIdempotentWithNew(t, ta, z.Datasets(), adapter)
		})
	}
}

func TestDependencies(t *testing.T) {
	failOnZFSPermissionDenied(t)

	tests := map[string]struct {
		depsFor string
		clones  []string

		wantDeps []string
	}{
		"Dataset has no child":  {depsFor: "rpool/ROOT/ubuntu/var/lib/apt"},
		"Dataset has one child": {depsFor: "rpool/ROOT/ubuntu/var/lib", wantDeps: []string{"rpool/ROOT/ubuntu/var/lib/apt"}},
		"Dataset has snapshots": {depsFor: "rpool/ROOT/ubuntu2", wantDeps: []string{"rpool/ROOT/ubuntu2@snap_u1", "rpool/ROOT/ubuntu2@snap_u2"}},

		"Snapshot has no child":                               {depsFor: "rpool/ROOT/ubuntu/opt/tools@snap_opt"},
		"Multiple snapshots on same datasets are independent": {depsFor: "rpool/ROOT/ubuntu2@snap_u1"},
		"Snapshot isn’t impacted by other children datasets":  {depsFor: "rpool/ROOT/ubuntu/var@snap_v1"},
		"Snapshots has a snapshot child":                      {depsFor: "rpool/ROOT/ubuntu/opt@snap_opt", wantDeps: []string{"rpool/ROOT/ubuntu/opt/tools@snap_opt"}},

		"Dataset has snapshots and children": {depsFor: "rpool/ROOT/ubuntu",
			wantDeps: []string{
				"rpool/ROOT/ubuntu@snap_r1",
				"rpool/ROOT/ubuntu/var@snap_v1", "rpool/ROOT/ubuntu/var/lib/apt", "rpool/ROOT/ubuntu/var/lib", "rpool/ROOT/ubuntu/var",
				"rpool/ROOT/ubuntu/opt/tools@snap_opt", "rpool/ROOT/ubuntu/opt/tools", "rpool/ROOT/ubuntu/opt@snap_opt", "rpool/ROOT/ubuntu/opt"}},

		"Snapshot with one clone":           {depsFor: "rpool/ROOT/ubuntu2@snap_u1", clones: []string{"rpool/ROOT/ubuntu2@snap_u1"}, wantDeps: []string{"rpool/ROOT/ubuntu2_cloned0"}},
		"Filesystem dataset with one clone": {depsFor: "rpool/ROOT/ubuntu2", clones: []string{"rpool/ROOT/ubuntu2@snap_u1"}, wantDeps: []string{"rpool/ROOT/ubuntu2@snap_u2", "rpool/ROOT/ubuntu2_cloned0", "rpool/ROOT/ubuntu2@snap_u1"}},

		"Clones on us and children": {depsFor: "rpool/ROOT/ubuntu/opt", clones: []string{"rpool/ROOT/ubuntu/opt@snap_opt"}, wantDeps: []string{
			"rpool/ROOT/ubuntu/opt_cloned0/tools", "rpool/ROOT/ubuntu/opt/tools@snap_opt",
			"rpool/ROOT/ubuntu/opt_cloned0", "rpool/ROOT/ubuntu/opt@snap_opt",
			"rpool/ROOT/ubuntu/opt/tools"}},
		"Child has clone": {depsFor: "rpool/ROOT/ubuntu/opt", clones: []string{"rpool/ROOT/ubuntu/opt/tools@snap_opt"}, wantDeps: []string{"rpool/ROOT/ubuntu/opt/tools_cloned0", "rpool/ROOT/ubuntu/opt/tools@snap_opt", "rpool/ROOT/ubuntu/opt/tools", "rpool/ROOT/ubuntu/opt@snap_opt"}},
		"Clone has clone": {depsFor: "rpool/ROOT/ubuntu/opt/tools",
			clones: []string{"rpool/ROOT/ubuntu/opt/tools@snap_opt", "rpool/ROOT/ubuntu/opt/tools_cloned0@snapcloned0"},
			wantDeps: []string{"rpool/ROOT/ubuntu/opt/tools_cloned1", "rpool/ROOT/ubuntu/opt/tools_cloned0@snapcloned0",
				"rpool/ROOT/ubuntu/opt/tools_cloned0", "rpool/ROOT/ubuntu/opt/tools@snap_opt"}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			dir, cleanup := testutils.TempDir(t)
			defer cleanup()

			adapter := testutils.GetLibZFS(t)
			fPools := testutils.NewFakePools(t, filepath.Join("testdata", "one_pool_n_datasets_n_children_n_snapshots_with_dependencies.yaml"), testutils.WithLibZFS(adapter))
			defer fPools.Create(dir)()

			z, err := zfs.New(context.Background(), zfs.WithLibZFS(adapter))
			if err != nil {
				t.Fatalf("expected no error but got: %v", err)
			}

			// Scan initial state for no-op
			trans, _ := z.NewTransaction(context.Background())
			defer trans.Done()

			// Prepare clone
			for i, clone := range tc.clones {
				// Take a snapshot to clone it (FIXME: this support should be in zfs_disk_handler)
				if i > 0 {
					err = trans.Snapshot(fmt.Sprintf("snapcloned%d", i-1), strings.Split(clone, "@")[0], false)
					assert.NoError(t, err, "error in setup")
				}
				err = trans.Clone(clone, fmt.Sprintf("cloned%d", i), false, true)
				assert.NoError(t, err, "error in setup")
			}

			d := z.DatasetByID(tc.depsFor)
			if d == nil {
				t.Fatalf("No dataset found matching %s", tc.depsFor)
			}

			nt := z.NewNoTransaction(context.Background())

			deps := nt.Dependencies(*d)

			// We can’t rely on the order of the original list, as we iterate over maps in the implementation.
			// However, we identified 3 rules to ensure that the dependency order (from leaf to root) is respected.

			depNames := make([]string, len(deps))
			for i, d := range deps {
				depNames[i] = d.Name
			}

			// rule 1: ensure that the 2 lists have the same elements
			if len(deps) != len(tc.wantDeps) {
				t.Fatalf("deps content doesn't have enough elements:\nGot:  %v\nWant: %v", deps, tc.wantDeps)
			} else {
				assert.ElementsMatch(t, tc.wantDeps, depNames, "didn't get matching dep list content")
			}

			// rule 2: ensure that all children (snapshots or filesystem datasets) appears before its parent
			assertChildrenBeforeParents(t, deps)

			// rule 3: ensure that a clone comes before its origin
			assertCloneComesBeforeItsOrigin(t, z, deps)
		})

	}
}
func TestTransactionsWithZFS(t *testing.T) {
	failOnZFSPermissionDenied(t)

	tests := map[string]struct {
		def           string
		doCreate      bool
		doSnapshot    bool
		doClone       bool
		doPromote     bool
		doSetProperty bool
		shouldErr     bool
		cancel        bool
	}{
		"Create only, success, Done":   {def: "layout1_for_transactions_tests.yaml", doCreate: true},
		"Create only, success, Cancel": {def: "layout1_for_transactions_tests.yaml", doCreate: true, cancel: true},
		"Create only, fail, Cancel":    {def: "layout1_for_transactions_tests.yaml", doCreate: true, shouldErr: true, cancel: true}, // cancel is a no-op as it errored
		"Create only, fail, No cancel": {def: "layout1_for_transactions_tests.yaml", doCreate: true, shouldErr: true},

		"Snapshot only, success, Done":   {def: "layout1_for_transactions_tests.yaml", doSnapshot: true},
		"Snapshot only, success, Cancel": {def: "layout1_for_transactions_tests.yaml", doSnapshot: true, cancel: true},
		"Snapshot only, fail, Cancel":    {def: "layout1_for_transactions_tests.yaml", doSnapshot: true, shouldErr: true, cancel: true}, // cancel is a no-op as it errored
		"Snapshot only, fail, No cancel": {def: "layout1_for_transactions_tests.yaml", doSnapshot: true, shouldErr: true},

		"Clone only, success, Done":   {def: "layout1_for_transactions_tests.yaml", doClone: true},
		"Clone only, success, Cancel": {def: "layout1_for_transactions_tests.yaml", doClone: true, cancel: true},
		// We unfortunately can't do those because we can't fail in the middle of Clone(), after some modification were done
		// The 2 failures are: either the dataset exists with suffix (won't clone anything) or missing intermediate snapshot
		// (won't even start cloning).
		// Avoid special casing the test code for no benefits.
		"Clone only, fail, Cancel":    {def: "layout1_for_transactions_tests.yaml", doClone: true, shouldErr: true, cancel: true},
		"Clone only, fail, No cancel": {def: "layout1_for_transactions_tests.yaml", doClone: true, shouldErr: true},

		"Promote only, success, Done":   {def: "layout1_for_transactions_tests.yaml", doPromote: true},
		"Promote only, success, Cancel": {def: "layout1_for_transactions_tests.yaml", doPromote: true, cancel: true},
		// We unfortunately can't do those because we can't fail in the middle of Promote(), after some modification were done

		"SetProperty only, success, Done":   {def: "layout1_for_transactions_tests.yaml", doSetProperty: true},
		"SetProperty only, success, Cancel": {def: "layout1_for_transactions_tests.yaml", doSetProperty: true, cancel: true},
		"SetProperty only, fail, Cancel":    {def: "layout1_for_transactions_tests.yaml", doSetProperty: true, shouldErr: true, cancel: true},
		"SetProperty only, fail, No cancel": {def: "layout1_for_transactions_tests.yaml", doSetProperty: true, shouldErr: true},

		// Destroy can't be in transactions

		"Multiple steps transaction, success, Done":   {def: "layout1_for_transactions_tests.yaml", doCreate: true, doSnapshot: true, doClone: true, doPromote: true, doSetProperty: true},
		"Multiple steps transaction, success, Cancel": {def: "layout1_for_transactions_tests.yaml", doCreate: true, doSnapshot: true, doClone: true, doPromote: true, doSetProperty: true, cancel: true},
		"Multiple steps transaction, fail, Cancel":    {def: "layout1_for_transactions_tests.yaml", doCreate: true, doSnapshot: true, doClone: true, doPromote: true, doSetProperty: true, shouldErr: true, cancel: true},
		"Multiple steps transaction, fail, No cancel": {def: "layout1_for_transactions_tests.yaml", doCreate: true, doSnapshot: true, doClone: true, doPromote: true, doSetProperty: true, shouldErr: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			dir, cleanup := testutils.TempDir(t)
			defer cleanup()

			ta := timeAsserter(time.Now())
			adapter := testutils.GetLibZFS(t)
			fPools := testutils.NewFakePools(t, filepath.Join("testdata", tc.def), testutils.WithLibZFS(adapter))
			defer fPools.Create(dir)()
			z, err := zfs.New(context.Background(), zfs.WithLibZFS(adapter))
			if err != nil {
				t.Fatalf("expected no error but got: %v", err)
			}
			initState := copyState(z)
			trans, cancel := z.NewTransaction(context.Background())
			defer trans.Done()

			state := initState
			var haveChanges bool

			// we issue multiple Create() to test the entire transactions (as not recursive)
			if tc.doCreate {
				datasetName := "rpool/ROOT/ubuntu_4242"
				if tc.shouldErr {
					// create a dataset without its parent will make it fail
					datasetName = "rpool/ROOT/ubuntu_4242/opt"
				}
				err := trans.Create(datasetName, "/home/foo", "on")
				if !tc.shouldErr && err != nil {
					t.Fatalf("create %q shouldn't have failed but it did: %v", datasetName, err)
				} else if tc.shouldErr && err == nil {
					t.Fatalf("creating %q should have returned an error but it didn't", datasetName)
				}
				if err != nil {
					assertDatasetsEquals(t, ta, state, z.Datasets())
				} else {
					assertDatasetsNotEquals(t, ta, state, z.Datasets())
					haveChanges = true
				}
				state = copyState(z)
			}

			if tc.doSnapshot {
				// We need to wait between setup and this snapshot, to ensure every snapshots are after latest
				// snapshot in testbed.
				time.Sleep(time.Second)
				snapName := "snap1"
				datasetName := "rpool/ROOT/ubuntu_1234"
				if tc.shouldErr {
					// an existing snapshot on var/lib exists and will make it fail
					snapName = "snap_r1"
					datasetName = "rpool/ROOT/ubuntu_1234/var"
				}
				err := trans.Snapshot(snapName, datasetName, true)
				if !tc.shouldErr && err != nil {
					t.Fatalf("taking snapshot shouldn't have failed but it did: %v", err)
				} else if tc.shouldErr && err == nil {
					t.Fatal("taking snapshot should have returned an error but it didn't")
				}
				if err != nil {
					assertDatasetsEquals(t, ta, state, z.Datasets())
				} else {
					assertDatasetsNotEquals(t, ta, state, z.Datasets())
					haveChanges = true
				}
				state = copyState(z)
			}

			if tc.doClone {
				name := "rpool/ROOT/ubuntu_1234@snap_r2"
				suffix := "5678"
				if tc.shouldErr {
					// rpool/ROOT/ubuntu_9999 exists
					suffix = "9999"
				}
				err := trans.Clone(name, suffix, false, true)
				if !tc.shouldErr && err != nil {
					t.Fatalf("cloning shouldn't have failed but it did: %v", err)
				} else if tc.shouldErr && err == nil {
					t.Fatal("cloning should have returned an error but it didn't")
				}
				if err != nil {
					assertDatasetsEquals(t, ta, state, z.Datasets())
				} else {
					assertDatasetsNotEquals(t, ta, state, z.Datasets())
					haveChanges = true
				}
				state = copyState(z)
			}

			if tc.doPromote {
				name := "rpool/ROOT/ubuntu_5678"
				if tc.shouldErr {
					// rpool/ROOT/ubuntu_1111 doesn't exists
					name = "rpool/ROOT/ubuntu_1111"
				} else {
					// Prepare cloning in its own transaction
					if !tc.doClone {
						trans2, _ := z.NewTransaction(context.Background())
						defer trans2.Done()

						err := trans2.Clone("rpool/ROOT/ubuntu_1234@snap_r2", "5678", false, true)
						if err != nil {
							t.Fatalf("couldnt clone to prepare dataset hierarchy: %v", err)
						}
						// Reset init state
						initState = copyState(z)
						trans2.Done()
						state = initState
					}
				}
				err := trans.Promote(name)
				if !tc.shouldErr && err != nil {
					t.Fatalf("promoting shouldn't have failed but it did: %v", err)
				} else if tc.shouldErr && err == nil {
					t.Fatal("promoting should have returned an error but it didn't")
				}
				if err != nil {
					assertDatasetsEquals(t, ta, state, z.Datasets())
				} else {
					assertDatasetsNotEquals(t, ta, state, z.Datasets())
					haveChanges = true
				}
				state = copyState(z)
			}

			if tc.doSetProperty {
				if tc.shouldErr {
					// this property isn't allowed
					assert.Panics(t, func() { trans.SetProperty("snapdir", "no", "rpool/ROOT/ubuntu_1234", false) }, "Panic was expected but didn't happen")
					assertDatasetsEquals(t, ta, state, z.Datasets())
				} else {
					err := trans.SetProperty(libzfs.BootfsProp, "no", "rpool/ROOT/ubuntu_1234", false)
					if err != nil {
						t.Fatalf("changing property shouldn't have failed but it did: %v", err)
					}
					assertDatasetsNotEquals(t, ta, state, z.Datasets())
					haveChanges = true
				}
			}

			// Final transaction states
			if tc.cancel {
				// Cancel: should get back to initial state
				cancel()
				haveChanges = false
			}
			trans.Done()
			// Done: should have commit the current state and be different from initial one
			if haveChanges {
				assertDatasetsNotEquals(t, ta, initState, z.Datasets())
			} else {
				assertDatasetsEquals(t, ta, initState, z.Datasets())
			}
			zfs.AssertNoZFSChildren(t, z)
			assertIdempotentWithNew(t, ta, z.Datasets(), adapter)
		})
	}
}

func TestTransaction(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		cancelCtxCalled         bool
		cancelTransactionCalled bool
		cancelJustAfterDone     bool
		doneDone                bool
		revertError             bool

		want string
	}{
		"Done without cancel": {},

		"Cancel with transaction cancelFunc":    {cancelTransactionCalled: true, want: "reverted"},
		"Cancel with parent context cancelFunc": {cancelCtxCalled: true, want: "reverted"},

		"Cancel just after done is a no-op": {cancelJustAfterDone: true},
		"Done just after done is a no-op":   {doneDone: true},
		"Revert error is logged":            {cancelTransactionCalled: true, revertError: true},
	}

	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			z, err := zfs.New(context.Background(), zfs.WithLibZFS(testutils.GetMockZFS(t)))
			if err != nil {
				t.Fatalf("couldn't create base ZFS object: %v", err)
			}
			ctx := context.Background()
			var cancelCtx context.CancelFunc
			if tc.cancelCtxCalled {
				ctx, cancelCtx = context.WithCancel(ctx)
				defer cancelCtx()
			}

			var result string

			trans, cancel := z.NewTransaction(ctx)
			trans.RegisterRevert(func() error {
				if tc.revertError {
					return errors.New("Revert returned an error")
				}
				result = "reverted"
				return nil
			})

			if tc.cancelCtxCalled {
				// cancel transaction via parent context cancel
				cancelCtx()
			} else if tc.cancelTransactionCalled {
				// cancel transaction via transaction context cancel
				cancel()
			}

			trans.Done()
			if tc.cancelJustAfterDone {
				cancel()
			}

			assert.Equal(t, tc.want, result, "result is not the expected value")

			if tc.doneDone {
				trans.Done()
				assert.Equal(t, tc.want, result, "result is not the expected value after second done")
			}
		})
	}
}

func TestTransactionContextIsSubContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	z, err := zfs.New(ctx, zfs.WithLibZFS(testutils.GetMockZFS(t)))
	if err != nil {
		t.Fatalf("couldn't create base ZFS object: %v", err)
	}
	trans, _ := z.NewTransaction(ctx)
	defer trans.Done()

	// We cancel parent ctx, transaction context (child) should be cancelled
	cancel()
	<-ctx.Done()

	select {
	case _, open := <-trans.Context().Done():
		if open {
			t.Error("child context isn't closed as parent is cancelled, but we received a value")
		}
	default:
		t.Error("child context isn't closed as parent is cancelled")
	}
}

func TestInvalidatedTransactionByDone(t *testing.T) {
	t.Parallel()

	z, err := zfs.New(context.Background(), zfs.WithLibZFS(testutils.GetMockZFS(t)))
	if err != nil {
		t.Fatalf("couldn't create base ZFS object: %v", err)
	}
	trans, _ := z.NewTransaction(context.Background())
	assert.NotPanics(t, trans.CheckValid, "transaction should be valid")

	trans.Done()

	assert.Panics(t, trans.CheckValid, "transaction should be invalidated")
}
func TestInvalidatedTransactionByCancel(t *testing.T) {
	t.Parallel()

	z, err := zfs.New(context.Background(), zfs.WithLibZFS(testutils.GetMockZFS(t)))
	if err != nil {
		t.Fatalf("couldn't create base ZFS object: %v", err)
	}
	trans, cancel := z.NewTransaction(context.Background())
	defer trans.Done()
	assert.NotPanics(t, trans.CheckValid, "transaction should be valid")

	cancel()
	trans.CancelPurged()

	assert.Panics(t, trans.CheckValid, "transaction should be invalidated")
}

// transformToReproducibleDatasetSlice applied transformation to ensure that the comparison is reproducible via
// DataSlices.
func transformToReproducibleDatasetSlice(t *testing.T, ta timeAsserter, got []*zfs.Dataset) zfs.DatasetSlice {
	t.Helper()

	// We don’t want to impact initial got slice order as it can be reused later on
	copyDS := make([]zfs.Dataset, 0, len(got))

	// Ensure datasets were created at expected range time and replace them with magic time.
	var ds []*zfs.Dataset
	for i, d := range got {
		copyDS = append(copyDS, *d)
		if !d.IsSnapshot {
			continue
		}
		ds = append(ds, &copyDS[i])
	}
	ta.assertAndReplaceCreationTimeInRange(t, ds)

	// Sort the golden file order to be reproducible.
	gotForGolden := zfs.DatasetSlice{DS: copyDS}
	sort.Sort(gotForGolden)
	return gotForGolden
}

// datasetsEquals prints a diff if datasets aren't equals and fails the test
func datasetsEquals(t *testing.T, want, got []zfs.Dataset) {
	t.Helper()

	// Actual diff assertion.
	if diff := cmp.Diff(want, got,
		cmpopts.IgnoreUnexported(zfs.Dataset{}),
		cmp.AllowUnexported(zfs.DatasetProp{})); diff != "" {
		t.Errorf("Datasets mismatch (-want +got):\n%s", diff)
	}
}

// datasetsNotEquals prints the struct if datasets are equals and fails the test
func datasetsNotEquals(t *testing.T, want, got []zfs.Dataset) {
	t.Helper()

	// Actual diff assertion.
	if diff := cmp.Diff(want, got,
		cmpopts.IgnoreUnexported(zfs.Dataset{}),
		cmp.AllowUnexported(zfs.DatasetProp{})); diff == "" {
		t.Error("datasets are equals where we expected not to:", pp.Sprint(want))
	}
}

// assertDatasetsToGolden compares (and update if needed) a slice of dataset got from a Datasets() for instance
// to a golden file.
func assertDatasetsToGolden(t *testing.T, ta timeAsserter, got []*zfs.Dataset) {
	t.Helper()

	gotForGolden := transformToReproducibleDatasetSlice(t, ta, got)

	// Get expected dataset list from golden file, update as needed.
	wantFromGolden := zfs.DatasetSlice{}
	testutils.LoadFromGoldenFile(t, gotForGolden, &wantFromGolden)
	want := []zfs.Dataset(wantFromGolden.DS)

	datasetsEquals(t, want, gotForGolden.DS)
}

// assertDatasetsEquals compares 2 slices of datasets, after ensuring they can be reproducible.
func assertDatasetsEquals(t *testing.T, ta timeAsserter, want, got []*zfs.Dataset) {
	t.Helper()

	wantCopy := transformToReproducibleDatasetSlice(t, ta, want).DS
	gotCopy := transformToReproducibleDatasetSlice(t, ta, got).DS

	datasetsEquals(t, wantCopy, gotCopy)
}

// assertDatasetsNotEquals compares 2 slices of datasets, ater ensuring they can be reproducible.
func assertDatasetsNotEquals(t *testing.T, ta timeAsserter, want, got []*zfs.Dataset) {
	t.Helper()

	wantCopy := transformToReproducibleDatasetSlice(t, ta, want).DS
	gotCopy := transformToReproducibleDatasetSlice(t, ta, got).DS

	datasetsNotEquals(t, wantCopy, gotCopy)
}

func assertIdempotentWithNew(t *testing.T, ta timeAsserter, inMemory []*zfs.Dataset, adapter libzfs.Interface) {
	t.Helper()

	// We should always have New() returning the same state than we manually updated
	newZ, err := zfs.New(context.Background(), zfs.WithLibZFS(adapter))
	if err != nil {
		t.Fatalf("expected no error but got: %v", err)
	}
	assertDatasetsEquals(t, ta, newZ.Datasets(), inMemory)
}

// assertChildrenBeforeParents ensure that all children (snapshots or filesystem datasets) appears before its parent
func assertChildrenBeforeParents(t *testing.T, deps []*zfs.Dataset) {
	t.Helper()

	// iterate on child
	for i, child := range deps {
		parent, snapshot := zfs.SplitSnapshotName(child.Name)
		if snapshot == "" {
			parent = child.Name[:strings.LastIndex(child.Name, "/")]
		}
		// search corresponding base from the start
		for j, candidate := range deps {
			if candidate.Name != parent {
				continue
			}
			if i > j {
				t.Errorf("Found child %s after its parent %s: %+v", child.Name, candidate.Name, deps)
			}
		}
	}
}

// assertCloneComesBeforeItsOrigin ensure that a clone comes before its origin
func assertCloneComesBeforeItsOrigin(t *testing.T, z *zfs.Zfs, deps []*zfs.Dataset) {
	t.Helper()

	for i, clone := range deps {
		if clone.Origin != "" {
			continue
		}

		// search corresponding origin from the start
		for j, candidate := range deps {
			if candidate.Name != clone.Origin {
				continue
			}
			if i > j {
				t.Errorf("Found clone %s after its origin snapshot %s: %+v", clone.Name, candidate.Name, deps)
			}
		}
	}
}

// timeAsserter ensures that dates will be between a start and end time
type timeAsserter time.Time

const currentMagicTime = 2000000000

// assertAndReplaceCreationTimeInRange ensure that last-used is between starts and endtime.
// It replaces those datasets last-used with the current fake "current time"
func (ta timeAsserter) assertAndReplaceCreationTimeInRange(t *testing.T, ds []*zfs.Dataset) {
	t.Helper()
	curr := time.Now().Unix()
	start := time.Time(ta).Unix()

	for _, r := range ds {
		// avoid transforming already set MagicTime
		if r.LastUsed == currentMagicTime {
			continue
		}

		if int64(r.LastUsed) < start || int64(r.LastUsed) > curr {
			t.Errorf("expected LastUsed time outside of range: %d. Start: %d, Current: %d", r.LastUsed, start, curr)
		} else {
			r.LastUsed = currentMagicTime
		}
	}
}

// failOnZFSPermissionDenied fail if we want to use the real ZFS system and don't have the permission for it
func failOnZFSPermissionDenied(t *testing.T) {
	t.Helper()

	if !testutils.UseSystemZFS() {
		return
	}

	u, err := user.Current()
	if err != nil {
		t.Fatal("can't get current user", err)
	}

	// in our default setup, only root users can interact with zfs kernel modules
	if u.Uid != "0" {
		t.Fatalf("you don't have permissions to interact with system zfs")
	}
}

// copyState freezes a given datasets layout by making copies
func copyState(z *zfs.Zfs) []*zfs.Dataset {
	datasets := z.Datasets()
	ds := make([]*zfs.Dataset, len(datasets))

	for i := range datasets {
		dcopy := *datasets[i]
		ds[i] = &dcopy
	}
	return ds
}
