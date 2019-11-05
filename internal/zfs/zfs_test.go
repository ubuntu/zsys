package zfs_test

import (
	"context"
	"io/ioutil"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"syscall"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/k0kubun/pp"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/testutils"
	"github.com/ubuntu/zsys/internal/zfs"
)

func init() {
	testutils.InstallUpdateFlag()
	config.SetVerboseMode(1)
}

func TestCreate(t *testing.T) {
	skipOnZFSPermissionDenied(t)

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
			dir, cleanup := tempDir(t)
			defer cleanup()

			ta := timeAsserter(time.Now())
			fPools := newFakePools(t, filepath.Join("testdata", tc.def))
			defer fPools.create(dir)()
			z := zfs.New(context.Background())
			defer z.Done()
			// Scan initial state for no-op
			var initState []zfs.Dataset
			if tc.wantErr {
				var err error
				initState, err = z.Scan()
				if err != nil {
					t.Fatalf("couldn't get initial state: %v", err)
				}
			}

			err := z.Create(tc.path, tc.mountpoint, tc.canmount)

			if err != nil {
				if !tc.wantErr {
					t.Fatalf("expected no error but got: %v", err)
				}
				// we don't return because we want to check that on error, Create() is a no-op
			}
			if err == nil && tc.wantErr {
				t.Fatal("expected an error but got none")
			}

			got, err := z.Scan()
			if err != nil {
				t.Fatalf("couldn't get final state: %v", err)
			}

			// check we didn't change anything on error
			if tc.wantErr {
				assertDatasetsEquals(t, ta, initState, got, true)
				return
			}
			assertDatasetsToGolden(t, ta, got, true)
		})
	}
}

func TestScan(t *testing.T) {
	skipOnZFSPermissionDenied(t)

	tests := map[string]struct {
		def     string
		mounted string

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
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			dir, cleanup := tempDir(t)
			defer cleanup()

			ta := timeAsserter(time.Now())
			fPools := newFakePools(t, filepath.Join("testdata", tc.def))
			defer fPools.create(dir)()

			if tc.mounted != "" {
				temp := filepath.Join(dir, "tempmount")
				if err := os.MkdirAll(temp, 0755); err != nil {
					t.Fatalf("couldn't create temporary mount point directory %q: %v", temp, err)
				}
				// zfs will unmount it when exporting the pool
				if err := syscall.Mount(tc.mounted, temp, "zfs", 0, "zfsutil"); err != nil {
					t.Fatalf("couldn't prepare and mount %q: %v", tc.mounted, err)
				}
			}

			z := zfs.New(context.Background())
			defer z.Done()
			got, err := z.Scan()
			if err != nil {
				if !tc.wantErr {
					t.Fatalf("expected no error but got: %v", err)
				}
				return
			}
			if tc.wantErr {
				t.Fatal("expected an error but got none")
			}

			assertDatasetsToGolden(t, ta, got, false)
		})
	}
}

func TestSnapshot(t *testing.T) {
	skipOnZFSPermissionDenied(t)

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
			dir, cleanup := tempDir(t)
			defer cleanup()

			ta := timeAsserter(time.Now())
			fPools := newFakePools(t, filepath.Join("testdata", tc.def))
			defer fPools.create(dir)()
			z := zfs.New(context.Background())
			defer z.Done()
			// Scan initial state for no-op
			var initState []zfs.Dataset
			var err error
			initState, err = z.Scan()
			if err != nil {
				t.Fatalf("couldn't get initial state: %v", err)
			}

			err = z.Snapshot(tc.snapshotName, tc.datasetName, tc.recursive)

			if err != nil {
				if !tc.wantErr {
					t.Fatalf("expected no error but got: %v", err)
				}
				// we don't return because we want to check that on error, Snapshot() is a no-op
			}
			if err == nil && tc.wantErr {
				t.Fatal("expected an error but got none")
			}

			got, err := z.Scan()
			if err != nil {
				t.Fatalf("couldn't get final state: %v", err)
			}

			if tc.isNoOp {
				assertDatasetsEquals(t, ta, initState, got, true)
				return
			}
			assertDatasetsToGolden(t, ta, got, true)
		})
	}
}

func TestClone(t *testing.T) {
	skipOnZFSPermissionDenied(t)

	tests := map[string]struct {
		def        string
		dataset    string
		suffix     string
		skipBootfs bool
		recursive  bool

		wantErr bool
		isNoOp  bool
	}{
		// TODO: Test case with user properties changed between snapshot and parent (with children inheriting)
		"Simple clone":    {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_1234@snap_r1", suffix: "5678"},
		"Recursive clone": {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_1234@snap_r1", suffix: "5678", recursive: true},
		"Simple clone ignore missing intermediate snapshots": {def: "layout1_missing_intermediate_snapshot.yaml", dataset: "rpool/ROOT/ubuntu_1234@snap_r1", suffix: "5678"},

		"Simple clone keeps canmount off as off":                      {def: "one_pool_n_datasets_one_snapshot_with_canmount_off.yaml", dataset: "rpool/ROOT/ubuntu@snap1", suffix: "5678"},
		"Simple clone keeps canmount noauto as noauto":                {def: "one_pool_n_datasets_one_snapshot_with_canmount_noauto.yaml", dataset: "rpool/ROOT/ubuntu@snap1", suffix: "5678"},
		"Simple clone set canmount on to noauto":                      {def: "one_pool_n_datasets_one_snapshot.yaml", dataset: "rpool/ROOT/ubuntu@snap1", suffix: "5678"},
		"Simple clone on non root local mountpoint keeps mountpoints": {def: "one_pool_n_datasets_one_snapshot_non_root.yaml", dataset: "rpool/ROOT/ubuntu@snap1", suffix: "5678"},

		"Simple clone on dataset without suffix":    {def: "layout1__one_pool_n_datasets_n_snapshots_without_suffix.yaml", dataset: "rpool/ROOT/ubuntu@snap_r1", suffix: "5678"},
		"Recursive clone on dataset without suffix": {def: "layout1__one_pool_n_datasets_n_snapshots_without_suffix.yaml", dataset: "rpool/ROOT/ubuntu@snap_r1", suffix: "5678", recursive: true},

		"Recursive missing some leaf snapshots":    {def: "layout1_missing_leaf_snapshot.yaml", dataset: "rpool/ROOT/ubuntu_1234@snap_r1", suffix: "5678", recursive: true},
		"Recursive missing intermediate snapshots": {def: "layout1_missing_intermediate_snapshot.yaml", dataset: "rpool/ROOT/ubuntu_1234@snap_r1", suffix: "5678", recursive: true, wantErr: true, isNoOp: true},

		"Allow cloning ignoring zsys bootfs": {def: "layout1_with_bootfs_already_cloned.yaml", dataset: "rpool/ROOT/ubuntu_1234@snap_r1", suffix: "5678", skipBootfs: true, recursive: true},

		"Snapshot doesn't exists":         {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_1234@doesntexists", suffix: "5678", wantErr: true, isNoOp: true},
		"Dataset doesn't exists":          {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_doesntexist@something", suffix: "5678", wantErr: true, isNoOp: true},
		"No suffix provided":              {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_1234@snap_r1", wantErr: true, isNoOp: true},
		"Suffixed dataset already exists": {def: "layout1_with_bootfs_already_cloned.yaml", dataset: "rpool/ROOT/ubuntu_1234@snap_r1", suffix: "5678", wantErr: true, isNoOp: true},
		"Clone on root fails":             {def: "one_pool_one_dataset_one_snapshot.yaml", dataset: "rpool@snap1", suffix: "5678", wantErr: true, isNoOp: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			dir, cleanup := tempDir(t)
			defer cleanup()

			ta := timeAsserter(time.Now())
			fPools := newFakePools(t, filepath.Join("testdata", tc.def))
			defer fPools.create(dir)()
			z := zfs.New(context.Background())
			defer z.Done()
			// Scan initial state for no-op
			var initState []zfs.Dataset
			var err error
			initState, err = z.Scan()
			if err != nil {
				t.Fatalf("couldn't get initial state: %v", err)
			}

			err = z.Clone(tc.dataset, tc.suffix, tc.skipBootfs, tc.recursive)

			if err != nil {
				if !tc.wantErr {
					t.Fatalf("expected no error but got: %v", err)
				}
				// we don't return because we want to check that on error, Clone() is a no-op
			}
			if err == nil && tc.wantErr {
				t.Fatal("expected an error but got none")
			}

			got, err := z.Scan()
			if err != nil {
				t.Fatalf("couldn't get final state: %v", err)
			}

			if tc.isNoOp {
				assertDatasetsEquals(t, ta, initState, got, true)
				return
			}
			assertDatasetsToGolden(t, ta, got, true)
		})
	}
}

func TestPromote(t *testing.T) {
	skipOnZFSPermissionDenied(t)

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
		"Promote with snapshots on origin":    {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_5678", cloneFrom: "rpool/ROOT/ubuntu_1234@snap_r1"},
		"Promote missing some leaf snapshots": {def: "layout1_missing_leaf_snapshot.yaml", dataset: "rpool/ROOT/ubuntu_5678", cloneFrom: "rpool/ROOT/ubuntu_1234@snap_r1"},

		"Promote already promoted hierarchy":  {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_1234", isNoOp: true},
		"Root of hierarchy already promoted":  {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_5678", cloneFrom: "rpool/ROOT/ubuntu_1234@snap_r1", alreadyPromoted: "rpool/ROOT/ubuntu_5678"},
		"Child of hierarchy already promoted": {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_5678", cloneFrom: "rpool/ROOT/ubuntu_1234@snap_r1", alreadyPromoted: "rpool/ROOT/ubuntu_5678/var"},

		"Dataset doesn't exists":                            {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_doesntexist", wantErr: true, isNoOp: true},
		"Promote a snapshot fails":                          {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_1234@snap_r1", wantErr: true, isNoOp: true},
		"Can't promote when missing intermediate snapshots": {def: "layout1_missing_intermediate_snapshot.yaml", dataset: "rpool/ROOT/ubuntu_5678", cloneFrom: "rpool/ROOT/ubuntu_1234@snap_r1", cloneOnlyOne: true, wantErr: true, isNoOp: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			dir, cleanup := tempDir(t)
			defer cleanup()

			ta := timeAsserter(time.Now())
			fPools := newFakePools(t, filepath.Join("testdata", tc.def))
			defer fPools.create(dir)()
			z := zfs.New(context.Background())
			defer z.Done()
			_, err := z.Scan() // Needed for cache
			if err != nil {
				t.Fatalf("couldn't get initial state: %v", err)
			}
			if tc.cloneFrom != "" {
				err := z.Clone(tc.cloneFrom, "5678", false, !tc.cloneOnlyOne)
				if err != nil {
					t.Fatalf("couldn't setup testbed when cloning: %v", err)
				}
			}
			if tc.alreadyPromoted != "" {
				err := z.Promote(tc.alreadyPromoted)
				if err != nil {
					t.Fatalf("couldn't setup testbed when prepromoting %q: %v", tc.alreadyPromoted, err)
				}
			}
			// Scan initial state for no-op
			var initState []zfs.Dataset
			if tc.isNoOp {
				var err error
				initState, err = z.Scan()
				if err != nil {
					t.Fatalf("couldn't get initial state: %v", err)
				}
			}

			err = z.Promote(tc.dataset)

			if err != nil {
				if !tc.wantErr {
					t.Fatalf("expected no error but got: %v", err)
				}
				// we don't return because we want to check that on error, Clone() is a no-op
			}
			if err == nil && tc.wantErr {
				t.Fatal("expected an error but got none")
			}

			got, err := z.Scan()
			if err != nil {
				t.Fatalf("couldn't get final state: %v", err)
			}

			if tc.isNoOp {
				assertDatasetsEquals(t, ta, initState, got, true)
				return
			}
			assertDatasetsToGolden(t, ta, got, true)
		})
	}
}

func TestDestroy(t *testing.T) {
	skipOnZFSPermissionDenied(t)

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
		"Hierarchy with snapshots":       {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_1234"},
		"Hierarchy with promoted clones": {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_1234", cloneFrom: "rpool/ROOT/ubuntu_1234@snap_r1", alreadyPromoted: "rpool/ROOT/ubuntu_5678"},

		"Leaf snapshot simple": {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_1234/var/lib/apt@snap_r1"},
		"Hierarchy snapshot":   {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_1234@snap_r1"},

		"Dataset doesn't exists":                    {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_doesntexist", wantErr: true, isNoOp: true},
		"Hierarchy with unpromoted clones":          {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_1234", cloneFrom: "rpool/ROOT/ubuntu_1234@snap_r1", wantErr: true, isNoOp: true},
		"Hierarchy with unpromoted clones non root": {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_1234", cloneFrom: "rpool/ROOT/ubuntu_1234/var@snap_r1", wantErr: true, isNoOp: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			dir, cleanup := tempDir(t)
			defer cleanup()

			ta := timeAsserter(time.Now())
			fPools := newFakePools(t, filepath.Join("testdata", tc.def))
			defer fPools.create(dir)()
			z := zfs.New(context.Background())
			defer z.Done()
			_, err := z.Scan() // Needed for cache
			if err != nil {
				t.Fatalf("couldn't get initial state: %v", err)
			}
			if tc.cloneFrom != "" {
				err := z.Clone(tc.cloneFrom, "5678", false, true)
				if err != nil {
					t.Fatalf("couldn't setup testbed when cloning: %v", err)
				}
			}
			if tc.alreadyPromoted != "" {
				err := z.Promote(tc.alreadyPromoted)
				if err != nil {
					t.Fatalf("couldn't setup testbed when prepromoting %q: %v", tc.alreadyPromoted, err)
				}
			}
			// Scan initial state for no-op
			var initState []zfs.Dataset
			if tc.isNoOp {
				var err error
				initState, err = z.Scan()
				if err != nil {
					t.Fatalf("couldn't get initial state: %v", err)
				}
			}

			err = z.Destroy(tc.dataset)

			if err != nil {
				if !tc.wantErr {
					t.Fatalf("expected no error but got: %v", err)
				}
				// we don't return because we want to check that on error, Clone() is a no-op
			}
			if err == nil && tc.wantErr {
				t.Fatal("expected an error but got none")
			}

			got, err := z.Scan()
			if err != nil {
				t.Fatalf("couldn't get final state: %v", err)
			}

			if tc.isNoOp {
				assertDatasetsEquals(t, ta, initState, got, true)
				return
			}
			assertDatasetsToGolden(t, ta, got, true)
		})
	}
}

func TestSetProperty(t *testing.T) {
	skipOnZFSPermissionDenied(t)

	tests := map[string]struct {
		def           string
		propertyName  string
		propertyValue string
		dataset       string
		force         bool

		wantErr bool
		isNoOp  bool
	}{
		"User property (local)":         {def: "one_pool_one_dataset_with_bootfsdatasets.yaml", propertyName: zfs.BootfsDatasetsProp, propertyValue: "SetProperty Value", dataset: "rpool"},
		"Authorized property (local)":   {def: "one_pool_one_dataset_with_bootfsdatasets.yaml", propertyName: zfs.CanmountProp, propertyValue: "noauto", dataset: "rpool"},
		"Authorized property (default)": {def: "one_pool_dataset_with_canmount_default.yaml", propertyName: zfs.CanmountProp, propertyValue: "noauto", dataset: "rpool/ubuntu"},
		"User property (none)":          {def: "one_pool_one_dataset_with_bootfsdatasets.yaml", propertyName: zfs.BootfsDatasetsProp, propertyValue: "SetProperty Value", dataset: "rpool"},
		// There is no authorized properties that can be "none" for now

		// Canmount prop is already checked in authorized
		"Mountpoint property": {def: "one_pool_one_dataset_with_bootfsdatasets.yaml", propertyName: zfs.MountPointProp, propertyValue: "/foo", dataset: "rpool"},

		"User property (inherit)":            {def: "one_pool_n_datasets_n_children_with_bootfsdatasets.yaml", propertyName: zfs.BootfsDatasetsProp, propertyValue: "SetProperty Value", dataset: "rpool/ROOT/ubuntu/var"},
		"User property (inherit but forced)": {def: "one_pool_n_datasets_n_children_with_bootfsdatasets.yaml", propertyName: zfs.BootfsDatasetsProp, propertyValue: "SetProperty Value", dataset: "rpool/ROOT/ubuntu/var", force: true},
		// There is no authorized properties that can be inherited

		"Property on snapshot (parent local)":                   {def: "one_pool_one_dataset_one_snapshot_with_bootfsdatasets.yaml", propertyName: zfs.BootfsDatasetsProp, propertyValue: "SetProperty Value", dataset: "rpool@snap1", wantErr: true, isNoOp: true},
		"Property on snapshot (parent none)":                    {def: "one_pool_one_dataset_one_snapshot.yaml", propertyName: zfs.BootfsDatasetsProp, propertyValue: "SetProperty Value", dataset: "rpool@snap1", wantErr: true, isNoOp: true},
		"Property on snapshot (parent inherit)":                 {def: "one_pool_n_datasets_n_children_n_snapshots_with_bootfsdatasets.yaml", propertyName: zfs.BootfsDatasetsProp, propertyValue: "SetProperty Value", dataset: "rpool/ROOT/ubuntu/var@snap_v1", wantErr: true, isNoOp: true},
		"User property on snapshot (parent inherit but forced)": {def: "one_pool_n_datasets_n_children_n_snapshots_with_bootfsdatasets.yaml", propertyName: zfs.BootfsDatasetsProp, propertyValue: "SetProperty Value", dataset: "rpool/ROOT/ubuntu/var@snap_v1", wantErr: true, isNoOp: true, force: true},

		"Unauthorized property":                          {def: "one_pool_one_dataset.yaml", propertyName: "snapdir", propertyValue: "/setproperty/value", dataset: "rpool", wantErr: true, isNoOp: true},
		"Dataset doesn't exists":                         {def: "one_pool_one_dataset.yaml", propertyName: zfs.BootfsDatasetsProp, propertyValue: "SetProperty Value", dataset: "rpool10", wantErr: true, isNoOp: true},
		"Authorized property on snapshot doesn't exists": {def: "one_pool_one_dataset_one_snapshot.yaml", propertyName: zfs.CanmountProp, propertyValue: "yes", dataset: "rpool@snap1", wantErr: true, isNoOp: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			dir, cleanup := tempDir(t)
			defer cleanup()

			ta := timeAsserter(time.Now())
			fPools := newFakePools(t, filepath.Join("testdata", tc.def))
			defer fPools.create(dir)()
			z := zfs.New(context.Background())
			defer z.Done()
			// Scan initial state for no-op
			var initState []zfs.Dataset
			if tc.isNoOp {
				var err error
				initState, err = z.Scan()
				if err != nil {
					t.Fatalf("couldn't get initial state: %v", err)
				}
			}

			err := z.SetProperty(tc.propertyName, tc.propertyValue, tc.dataset, tc.force)

			if err != nil {
				if !tc.wantErr {
					t.Fatalf("expected no error but got: %v", err)
				}
				// we don't return because we want to check that on error, SetProperty() is a no-op
			}
			if err == nil && tc.wantErr {
				t.Fatal("expected an error but got none")
			}

			got, err := z.Scan()
			if err != nil {
				t.Fatalf("couldn't get final state: %v", err)
			}

			if tc.isNoOp {
				assertDatasetsEquals(t, ta, initState, got, true)
				return
			}
			assertDatasetsToGolden(t, ta, got, true)
		})
	}
}

func TestTransactions(t *testing.T) {
	skipOnZFSPermissionDenied(t)

	tests := map[string]struct {
		def           string
		doCreate      bool
		doSnapshot    bool
		doClone       bool
		doPromote     bool
		doSetProperty bool
		doDestroy     bool
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

		"Clone only, success, Done":   {def: "layout1_for_transactions_tests.yaml", doClone: true, cancel: true},
		"Clone only, success, Cancel": {def: "layout1_for_transactions_tests.yaml", doClone: true},
		// We unfortunately can't do those because we can't fail in the middle of Clone(), after some modification were done
		// The 2 failures are: either the dataset exists with suffix (won't clone anything) or missing intermediate snapshot
		// (won't even start cloning).
		// Avoid special casing the test code for no benefits.
		//"Clone only, fail, Cancel":    {def: "layout1_for_transactions_tests.yaml", doClone: true, shouldErr: true, cancel: true},
		//"Clone only, fail, No cancel": {def: "layout1_for_transactions_tests.yaml", doClone: true, shouldErr: true},

		"Promote only, success, Done":   {def: "layout1_for_transactions_tests.yaml", doPromote: true, cancel: true},
		"Promote only, success, Cancel": {def: "layout1_for_transactions_tests.yaml", doPromote: true},
		// We unfortunately can't do those because we can't fail in the middle of Promote(), after some modification were done

		"SetProperty only, success, Done":   {def: "layout1_for_transactions_tests.yaml", doSetProperty: true, cancel: true},
		"SetProperty only, success, Cancel": {def: "layout1_for_transactions_tests.yaml", doSetProperty: true},
		// We unfortunately can't do those because we can't fail in the middle of SetProperty(), after some modification were done

		// Destroy can't be in transactions
		"Destroy, failed before doing anything": {def: "layout1_for_transactions_tests.yaml", doDestroy: true},

		"Multiple steps transaction, success, Done":   {def: "layout1_for_transactions_tests.yaml", doCreate: true, doSnapshot: true, doClone: true, doPromote: true, doSetProperty: true},
		"Multiple steps transaction, success, Cancel": {def: "layout1_for_transactions_tests.yaml", doCreate: true, doSnapshot: true, doClone: true, doPromote: true, doSetProperty: true, cancel: true},
		"Multiple steps transaction, fail, Cancel":    {def: "layout1_for_transactions_tests.yaml", doCreate: true, doSnapshot: true, doClone: true, doPromote: true, doSetProperty: true, shouldErr: true, cancel: true},
		"Multiple steps transaction, fail, No cancel": {def: "layout1_for_transactions_tests.yaml", doCreate: true, doSnapshot: true, doClone: true, doPromote: true, doSetProperty: true, shouldErr: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			dir, cleanup := tempDir(t)
			defer cleanup()

			ta := timeAsserter(time.Now())
			fPools := newFakePools(t, filepath.Join("testdata", tc.def))
			defer fPools.create(dir)()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			z := zfs.New(ctx)
			defer z.Done()

			initState, err := z.Scan()
			if err != nil {
				t.Fatalf("couldn't get initial state: %v", err)
			}
			state := initState
			var haveChanges bool

			// we issue multiple Create() to test the entire transactions (as not recursive)
			if tc.doCreate {
				datasetName := "rpool/ROOT/ubuntu_4242"
				if tc.shouldErr {
					// create a dataset without its parent will make it fail
					datasetName = "rpool/ROOT/ubuntu_4242/opt"
				}
				err := z.Create(datasetName, "/home/foo", "on")
				if !tc.shouldErr && err != nil {
					t.Fatalf("create %q shouldn't have failed but it did: %v", datasetName, err)
				} else if tc.shouldErr && err == nil {
					t.Fatalf("creating %q should have returned an error but it didn't", datasetName)
				}
				newState, errScan := z.Scan()
				if errScan != nil {
					t.Fatalf("couldn't get state after create: %v", errScan)
				}
				if err != nil {
					assertDatasetsEquals(t, ta, state, newState, true)
				} else {
					assertDatasetsNotEquals(t, ta, state, newState, true)
					haveChanges = true
				}
				state = newState
			}

			if tc.doSnapshot {
				snapName := "snap1"
				datasetName := "rpool/ROOT/ubuntu_1234"
				if tc.shouldErr {
					// an existing snapshot on var/lib exists and will make it fail
					snapName = "snap_r1"
					datasetName = "rpool/ROOT/ubuntu_1234/var"
				}
				err := z.Snapshot(snapName, datasetName, true)
				if !tc.shouldErr && err != nil {
					t.Fatalf("taking snapshot shouldn't have failed but it did: %v", err)
				} else if tc.shouldErr && err == nil {
					t.Fatal("taking snapshot should have returned an error but it didn't")
				}
				newState, errScan := z.Scan()
				if errScan != nil {
					t.Fatalf("couldn't get state after snapshot: %v", errScan)
				}
				if err != nil {
					assertDatasetsEquals(t, ta, state, newState, true)
				} else {
					assertDatasetsNotEquals(t, ta, state, newState, true)
					haveChanges = true
				}
				state = newState
			}

			if tc.doClone {
				name := "rpool/ROOT/ubuntu_1234@snap_r2"
				suffix := "5678"
				if tc.shouldErr {
					// rpool/ROOT/ubuntu_9999 exists
					suffix = "9999"
				}
				err := z.Clone(name, suffix, false, true)
				if !tc.shouldErr && err != nil {
					t.Fatalf("cloning shouldn't have failed but it did: %v", err)
				} else if tc.shouldErr && err == nil {
					t.Fatal("cloning should have returned an error but it didn't")
				}
				newState, errScan := z.Scan()
				if errScan != nil {
					t.Fatalf("couldn't get state after snapshot: %v", errScan)
				}
				if err != nil {
					assertDatasetsEquals(t, ta, state, newState, true)
				} else {
					assertDatasetsNotEquals(t, ta, state, newState, true)
					haveChanges = true
				}
				state = newState
			}

			if tc.doPromote {
				name := "rpool/ROOT/ubuntu_5678"
				if tc.shouldErr {
					// rpool/ROOT/ubuntu_1111 doesn't exists
					name = "rpool/ROOT/ubuntu_1111"
				} else {
					// Prepare cloning in its own transaction
					if !tc.doClone {
						z2 := zfs.New(context.Background())
						defer z2.Done()
						err := z2.Clone("rpool/ROOT/ubuntu_1234@snap_r2", "5678", false, true)
						if err != nil {
							t.Fatalf("couldnt clone to prepare dataset hierarchy: %v", err)
						}
						// Reset init state
						initState, err = z.Scan()
						if err != nil {
							t.Fatalf("couldn't get initial state: %v", err)
						}
						z2.Done()
						state = initState
					}
				}
				err := z.Promote(name)
				if !tc.shouldErr && err != nil {
					t.Fatalf("promoting shouldn't have failed but it did: %v", err)
				} else if tc.shouldErr && err == nil {
					t.Fatal("promoting should have returned an error but it didn't")
				}
				newState, errScan := z.Scan()
				if errScan != nil {
					t.Fatalf("couldn't get state after snapshot: %v", errScan)
				}
				if err != nil {
					assertDatasetsEquals(t, ta, state, newState, true)
				} else {
					assertDatasetsNotEquals(t, ta, state, newState, true)
					haveChanges = true
				}
				state = newState
			}

			if tc.doDestroy {
				if err := z.Destroy("rpool/ROOT/ubuntu_1234"); err == nil {
					t.Fatalf("expected destroy to not work in transactions, but it returned no error")
				}
				// Expect no modifications: the only case we can test is a failing one
				newState, errScan := z.Scan()
				if errScan != nil {
					t.Fatalf("couldn't get state after destruction: %v", errScan)
				}
				assertDatasetsEquals(t, ta, state, newState, true)
				return
			}

			if tc.doSetProperty {
				propertyName := zfs.BootfsProp
				if tc.shouldErr {
					// this property isn't allowed
					propertyName = "snapdir"
				}
				err := z.SetProperty(propertyName, "no", "rpool/ROOT/ubuntu_1234", false)
				if !tc.shouldErr && err != nil {
					t.Fatalf("changing property shouldn't have failed but it did: %v", err)
				} else if tc.shouldErr && err == nil {
					t.Fatal("changing property should have returned an error but it didn't")
				}
				newState, errScan := z.Scan()
				if errScan != nil {
					t.Fatalf("couldn't get state after snapshot: %v", errScan)
				}
				if err != nil {
					assertDatasetsEquals(t, ta, state, newState, true)
				} else {
					assertDatasetsNotEquals(t, ta, state, newState, true)
					haveChanges = true
				}
				state = newState
			}

			// Final transaction states
			if tc.cancel {
				// Cancel: should get back to initial state
				cancel()
				haveChanges = false
			}
			z.Done()
			finalState, errScan := z.Scan()
			if errScan != nil {
				t.Fatalf("couldn't get finale state: %v", errScan)
			}

			// Done: should have commit the current state and be different from initial one
			if haveChanges {
				assertDatasetsNotEquals(t, ta, initState, finalState, true)
			} else {
				assertDatasetsEquals(t, ta, initState, finalState, true)
			}
		})
	}
}

func TestNewTransaction(t *testing.T) {
	skipOnZFSPermissionDenied(t)

	tests := map[string]struct {
		errSnapshot     bool
		cancellable     bool
		cancel          bool
		cancelAfterDone bool
	}{
		"Everything pass":                       {},
		"Cancel":                                {cancellable: true, cancel: true},
		"Cancellable context but didn't cancel": {cancellable: true, cancel: false},
		"Cancel after done call already committed the change": {cancellable: true, cancel: true, cancelAfterDone: true},

		"Error reverts automatically intermediate changes, but not all":  {errSnapshot: true},
		"Error with cancel reverted everything":                          {errSnapshot: true, cancellable: true, cancel: true},
		"Error with cancel after done only committed some state changes": {errSnapshot: true, cancellable: true, cancel: true, cancelAfterDone: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			dir, cleanup := tempDir(t)
			defer cleanup()

			ta := timeAsserter(time.Now())
			fPools := newFakePools(t, filepath.Join("testdata", "layout1__one_pool_n_datasets_n_snapshots.yaml"))
			defer fPools.create(dir)()
			ctx := context.Background()
			var cancel context.CancelFunc
			if tc.cancellable {
				ctx, cancel = context.WithCancel(ctx)
				defer cancel()
			}

			z := zfs.New(ctx)
			defer z.Done()

			// Scan initial state for no-op
			initState, err := z.Scan()
			if err != nil {
				t.Fatalf("couldn't get initial state: %v", err)
			}

			if err := z.Create("rpool/ROOT/New", "/", "on"); err != nil {
				t.Fatalf("didn't expect a failure but got: %v", err)
			}

			intermediateState, err := z.Scan()
			if err != nil {
				t.Fatalf("couldn't get initial state: %v", err)
			}

			snapshotName := "new_snap"
			if tc.errSnapshot {
				// this snapshot already exists on a subdatasets
				snapshotName = "snap_r1"
			}
			err = z.Snapshot(snapshotName, "rpool", true)
			if !tc.errSnapshot && err != nil {
				t.Fatalf("didn't expect an error on snapshot but got: %v", err)
			} else if tc.errSnapshot && err == nil {
				t.Fatalf("did expect an error on snapshot but got none")
			}

			afterSnapshotState, err := z.Scan()
			if err != nil {
				t.Fatalf("couldn't get initial state: %v", err)
			}

			// check that an error in the recursive snapshot call has always been reverted (having a cancellable or non cancellable transactions)
			if tc.errSnapshot {
				assertDatasetsEquals(t, ta, intermediateState, afterSnapshotState, true)
			} else {
				assertDatasetsNotEquals(t, ta, intermediateState, afterSnapshotState, true)
			}

			if tc.cancellable && tc.cancel {
				if tc.cancelAfterDone {
					z.Done() // this should make the next cancel a no-op
				}
				cancel()
				z.Done() // wait for cancel() to return
			}

			finaleState, err := z.Scan()
			if err != nil {
				t.Fatalf("couldn't get initial state: %v", err)
			}

			// cancel should have reverted everything, including snapshot transaction if didn't fail
			if !tc.cancelAfterDone && tc.cancellable && tc.cancel {
				assertDatasetsEquals(t, ta, initState, finaleState, true)
				return
			}
			// no cancel or cancel after Done is too late: we should have changes
			assertDatasetsNotEquals(t, ta, initState, finaleState, true)
		})
	}
}

// transformToReproducibleDatasetSlice applied transformation to ensure that the comparison is reproducible via
// DataSlices.
func transformToReproducibleDatasetSlice(t *testing.T, ta timeAsserter, got []zfs.Dataset, includePrivate bool) zfs.DatasetSlice {
	t.Helper()

	// Ensure datasets were created at expected range time and replace them with magic time.
	var ds []*zfs.Dataset
	for k := range got {
		if !got[k].IsSnapshot {
			continue
		}
		ds = append(ds, &got[k])
	}
	ta.assertAndReplaceCreationTimeInRange(t, ds)

	// Sort the golden file order to be reproducible.
	gotForGolden := zfs.DatasetSlice{DS: got, IncludePrivate: includePrivate}
	sort.Sort(gotForGolden)
	return gotForGolden
}

// datasetsEquals prints a diff if datasets aren't equals and fails the test
func datasetsEquals(t *testing.T, want, got []zfs.Dataset, includePrivate bool) {
	t.Helper()

	// Actual diff assertion.
	privateOpt := cmpopts.IgnoreUnexported(zfs.DatasetProp{})
	if includePrivate {
		privateOpt = cmp.AllowUnexported(zfs.DatasetProp{})
	}
	if diff := cmp.Diff(want, got, privateOpt); diff != "" {
		t.Errorf("Scan() mismatch (-want +got):\n%s", diff)
	}
}

// datasetsNotEquals prints the struct if datasets are equals and fails the test
func datasetsNotEquals(t *testing.T, want, got []zfs.Dataset, includePrivate bool) {
	t.Helper()

	// Actual diff assertion.
	privateOpt := cmpopts.IgnoreUnexported(zfs.DatasetProp{})
	if includePrivate {
		privateOpt = cmp.AllowUnexported(zfs.DatasetProp{})
	}
	if diff := cmp.Diff(want, got, privateOpt); diff == "" {
		t.Error("datasets are equals where we expected not to:", pp.Sprint(want))
	}
}

// assertDatasetsToGolden compares (and update if needed) a slice of dataset got from a Scan() for instance
// to a golden file.
// We can optionnally include private fields in the comparison and saving.
func assertDatasetsToGolden(t *testing.T, ta timeAsserter, got []zfs.Dataset, includePrivate bool) {
	t.Helper()

	gotForGolden := transformToReproducibleDatasetSlice(t, ta, got, includePrivate)
	got = gotForGolden.DS

	// Get expected dataset list from golden file, update as needed.
	wantFromGolden := zfs.DatasetSlice{IncludePrivate: includePrivate}
	testutils.LoadFromGoldenFile(t, gotForGolden, &wantFromGolden)
	want := []zfs.Dataset(wantFromGolden.DS)

	datasetsEquals(t, want, got, includePrivate)
}

// assertDatasetsEquals compares 2 slices of datasets, after ensuring they can be reproducible.
// We can optionnally include private fields in the comparison.
func assertDatasetsEquals(t *testing.T, ta timeAsserter, want, got []zfs.Dataset, includePrivate bool) {
	t.Helper()

	want = transformToReproducibleDatasetSlice(t, ta, want, includePrivate).DS
	got = transformToReproducibleDatasetSlice(t, ta, got, includePrivate).DS

	datasetsEquals(t, want, got, includePrivate)
}

// assertDatasetsNotEquals compares 2 slices of datasets, ater ensuring they can be reproducible.
// We can optionnally include private fields in the comparison.
func assertDatasetsNotEquals(t *testing.T, ta timeAsserter, want, got []zfs.Dataset, includePrivate bool) {
	t.Helper()

	want = transformToReproducibleDatasetSlice(t, ta, want, includePrivate).DS
	got = transformToReproducibleDatasetSlice(t, ta, got, includePrivate).DS

	datasetsNotEquals(t, want, got, includePrivate)
}

func tempDir(t *testing.T) (string, func()) {
	t.Helper()

	dir, err := ioutil.TempDir("", "zsystest-")
	if err != nil {
		t.Fatal("can't create temporary directory", err)
	}
	return dir, func() {
		if err = os.RemoveAll(dir); err != nil {
			t.Error("can't clean temporary directory", err)
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
			t.Errorf("expected snapshot time outside of range: %d", r.LastUsed)
		} else {
			r.LastUsed = currentMagicTime
		}
	}
}

// skipOnZFSPermissionDenied skips the tests if the current user can't create zfs pools, datasets…
func skipOnZFSPermissionDenied(t *testing.T) {
	t.Helper()

	u, err := user.Current()
	if err != nil {
		t.Fatal("can't get current user", err)
	}

	// in our default setup, only root users can interact with zfs kernel modules
	if u.Uid != "0" {
		t.Skip("skipping, you don't have permissions to interact with system zfs")
	}
}
