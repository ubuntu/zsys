package machines_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/k0kubun/pp"
	"github.com/stretchr/testify/assert"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/machines"
	"github.com/ubuntu/zsys/internal/testutils"
	"github.com/ubuntu/zsys/internal/zfs"
	libzfsadapter "github.com/ubuntu/zsys/internal/zfs/libzfs"
	"github.com/ubuntu/zsys/internal/zfs/libzfs/mock"
)

func TestNew(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		def            string
		cmdline        string
		mountedDataset string
	}{
		"One machine, one dataset":            {def: "d_one_machine_one_dataset.yaml"},
		"One disabled machine":                {def: "d_one_disabled_machine.yaml"},
		"One machine with children":           {def: "d_one_machine_with_children.yaml"},
		"One machine with unordered children": {def: "d_one_machine_with_children_unordered.yaml"},

		"One machine, attach user datasets to machine": {def: "m_with_userdata.yaml"},
		"One machine, attach boot to machine":          {def: "m_with_separate_boot.yaml"},
		"One machine, with persistent datasets":        {def: "m_with_persistent.yaml"},

		"One machine with children, snapshot on subdataset": {def: "d_one_machine_with_children_snapshot_on_subdataset.yaml"},

		// Machine <-> snapshot interactions
		"One machine with one snapshot":                              {def: "d_one_machine_with_one_snapshot.yaml"},
		"One machine with snapshot having less datasets than parent": {def: "d_one_machine_with_snapshot_less_datasets.yaml"},
		"One machine with snapshot having more datasets than parent": {def: "d_one_machine_with_snapshot_more_datasets.yaml"}, // This is actually one dataset being canmount=off

		// Machine <-> clones interactions
		// FIXME: The clone is deleted from got
		"One machine with one clone":                            {def: "d_one_machine_with_clone_dataset.yaml"},
		"One machine with one clone named before":               {def: "d_one_machine_with_clone_named_before.yaml"},
		"One machine with clones and snapshot on user datasets": {def: "m_with_clones_snapshots_userdata.yaml"},

		// NOTE: This case cannot happen and cannot be represented in the yaml test data
		//"One machine with a missing clone results in ignored machine (ZFS error)": {def: "d_one_machine_missing_clone.json"},

		// Zsys system special cases
		"One machine, one dataset, non zsys":   {def: "d_one_machine_one_dataset_non_zsys.yaml"},
		"Two machines, one zsys, one non zsys": {def: "d_two_machines_one_zsys_one_non_zsys.yaml"},

		// Last used special cases
		"One machine, no last used":                              {def: "d_one_machine_one_dataset_no_lastused.yaml"},
		"One machine with children, last used from root is used": {def: "d_one_machine_with_children_all_with_lastused.yaml"},

		// Boot special cases
		// TODO: separate boot and internal boot dataset? See grub
		"Two machines maps with different boot datasets":                 {def: "m_two_machines_with_separate_boot.yaml"},
		"Boot dataset attached to nothing":                               {def: "m_with_unlinked_boot.yaml"}, // boots are still listed in the "all" list for switch to noauto.
		"Boot dataset attached to nothing but ignored with canmount off": {def: "m_with_unlinked_boot_canmount_off.yaml"},
		"Snapshot with boot dataset":                                     {def: "m_snapshot_with_separate_boot.yaml"},
		"Clone with boot dataset":                                        {def: "m_clone_with_separate_boot.yaml"},
		"Snapshot with boot dataset with children":                       {def: "m_snapshot_with_separate_boot_with_children.yaml"},
		"Clone with boot dataset with children":                          {def: "m_clone_with_separate_boot_with_children.yaml"},
		"Clone with boot dataset with children manually created":         {def: "m_clone_with_separate_boot_with_children_manually_created.yaml"},

		// Userdata special cases
		"Two machines maps with different user datasets":                         {def: "m_two_machines_with_different_userdata.yaml"},
		"Two machines maps with same user datasets":                              {def: "m_two_machines_with_same_userdata.yaml"},
		"User dataset attached to nothing":                                       {def: "m_with_unlinked_userdata.yaml"},
		"User dataset attached to nothing but ignored with canmount off":         {def: "m_with_unlinked_userdata_canmount_off.yaml"},
		"Snapshot with user dataset":                                             {def: "m_snapshot_with_userdata.yaml"},
		"Clone with user dataset":                                                {def: "m_clone_with_userdata.yaml"},
		"Snapshot with user dataset with children":                               {def: "m_snapshot_with_userdata_with_children.yaml"},
		"Clone with user dataset with children":                                  {def: "m_clone_with_userdata_with_children.yaml"},
		"Clone with user dataset with children manually created":                 {def: "m_clone_with_userdata_with_children_manually_created.yaml"},
		"Userdata with children associated only to one state":                    {def: "m_with_userdata_child_associated_one_state.yaml"},
		"Userdata is linked to no machines":                                      {def: "m_with_userdata_linked_to_no_machines.yaml"},
		"Userdata is grouped via its snapshot":                                   {def: "m_with_userdata_snapshot_attached.yaml"},
		"Userdata is grouped via its snapshot which has a clone":                 {def: "m_with_userdata_snapshot_attached_with_clone.yaml"},
		"Userdata is grouped via its clone":                                      {def: "m_with_userdata_clone_attached.yaml"},
		"Userdata is grouped via its snapshot on clone":                          {def: "m_with_userdata_group_via_snapshot_on_clone.yaml"},
		"Userdata is grouped via its clone even with children clone":             {def: "m_with_userdata_group_via_its_clone_with_child_clone.yaml"},
		"Userdata is grouped via its snapshot on clone even with children clone": {def: "m_with_userdata_group_via_its_snapshot_on_clone_with_child_clone.yaml"},
		"Userdata is grouped via its secondary clone":                            {def: "m_with_userdata_group_via_its_secondary_clone.yaml"},
		"Userdata is grouped via its snapshot on secondary clone":                {def: "m_with_userdata_group_via_its_snapshot_on_secondary_clone.yaml"},

		// Userdata user snapshots
		"Userdata has a user snapshot":              {def: "m_with_userdata_user_snapshot.yaml"},
		"Userdata with underscore in snapshot name": {def: "m_with_userdata_snapshotname_with_underscore.yaml"},

		// Persistent special cases
		"One machine, with persistent disabled":  {def: "m_with_persistent_canmount_noauto.yaml"},
		"Two machines have the same persistents": {def: "m_two_machines_with_persistent.yaml"},
		"Snapshot has the same persistents":      {def: "m_snapshot_with_persistent.yaml"},
		"Clone has the same persistents":         {def: "m_clone_with_persistent.yaml"},

		// Bpool special cases
		"Machine with bpool with children and snapshots": {def: "state_snapshot_with_userdata_n_system_clones.yaml"},

		// Limit case with no machines
		"No machine": {def: "d_no_machine.yaml"},
		"No dataset": {def: "d_no_dataset.yaml"},

		// Real machine use cases
		"zsys layout desktop, one machine":                                 {def: "m_layout1_one_machine.yaml"},
		"zsys layout server, one machine":                                  {def: "m_layout2_one_machine.yaml"},
		"zsys layout desktop with snapshots and clones, multiple machines": {def: "m_layout1_machines_with_snapshots_clones.yaml"},
		"zsys layout desktop with cloning in progress, multiple machines":  {def: "m_layout1_machines_with_snapshots_clones_reverting.yaml"},
		"zsys layout server with snapshots and clones, multiple machines":  {def: "m_layout2_machines_with_snapshots_clones.yaml"},
		"zsys layout server with cloning in progress, multiple machines":   {def: "m_layout2_machines_with_snapshots_clones_reverting.yaml"},

		// cmdline selection
		"Select existing dataset machine":            {def: "d_one_machine_one_dataset.yaml", cmdline: generateCmdLine("rpool")},
		"Select correct machine":                     {def: "d_two_machines_one_dataset.yaml", cmdline: generateCmdLine("rpool2")},
		"Select main machine with snapshots/clones":  {def: "m_clone_with_persistent.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_1234")},
		"Select snapshot use mounted system dataset": {def: "m_clone_with_persistent.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_1234@snap1"), mountedDataset: "rpool/ROOT/ubuntu_1234"},
		"Select clone":                               {def: "m_clone_with_persistent.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Selected machine doesn't exist":             {def: "d_one_machine_one_dataset.yaml", cmdline: generateCmdLine("foo")},
		"Select existing dataset but not a machine":  {def: "m_with_persistent.yaml", cmdline: generateCmdLine("rpool/ROOT")},

		// Error cases
		"Clone, origin doesn't exist": {def: "m_clone_origin_doesnt_exist.yaml"},
	}

	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir, cleanup := testutils.TempDir(t)
			defer cleanup()

			libzfs := testutils.GetMockZFS(t)
			fPools := testutils.NewFakePools(t, filepath.Join("testdata", tc.def), testutils.WithLibZFS(libzfs))
			defer fPools.Create(dir)()

			if tc.mountedDataset != "" {
				lzfs := libzfs.(*mock.LibZFS)
				lzfs.SetDatasetAsMounted(tc.mountedDataset, true)
			}

			got, err := machines.New(context.Background(), tc.cmdline, machines.WithLibZFS(libzfs))
			if err != nil {
				t.Error("expected success but got an error scanning for machines", err)
			}
			assertMachinesToGolden(t, got)
		})
	}
}

func TestIdempotentNew(t *testing.T) {
	t.Parallel()
	dir, cleanup := testutils.TempDir(t)
	defer cleanup()

	libzfs := testutils.GetMockZFS(t)
	fPools := testutils.NewFakePools(t, filepath.Join("testdata", "m_layout2_machines_with_snapshots_clones.yaml"), testutils.WithLibZFS(libzfs))
	defer fPools.Create(dir)()

	got1, err := machines.New(context.Background(), generateCmdLine("rpool/ROOT/ubuntu_5678"), machines.WithLibZFS(libzfs))
	if err != nil {
		t.Error("expected success but got an error at first scan on machines", err)
	}
	got2, err := machines.New(context.Background(), generateCmdLine("rpool/ROOT/ubuntu_5678"), machines.WithLibZFS(libzfs))
	if err != nil {
		t.Error("expected success but got an error at second scan on machines", err)
	}
	assertMachinesEquals(t, got1, got2)
}

func TestBoot(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		def            string
		cmdline        string
		mountedDataset string

		cloneErr       bool
		scanErr        bool
		setPropertyErr bool

		wantErr bool
		isNoOp  bool
	}{
		"One machine one dataset zsys":      {def: "d_one_machine_one_dataset.yaml", cmdline: generateCmdLine("rpool"), isNoOp: true},
		"One machine one dataset non zsys":  {def: "d_one_machine_one_dataset_non_zsys.yaml", cmdline: generateCmdLine("rpool"), isNoOp: true},
		"One machine one dataset, no match": {def: "d_one_machine_one_dataset.yaml", cmdline: generateCmdLine("rpoolfake"), isNoOp: true},

		// Two machines tests
		"Two machines, keep active":                        {def: "m_two_machines_simple.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_1234"), isNoOp: true},
		"Two machines, simple switch":                      {def: "m_two_machines_simple.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Two machines, with children":                      {def: "m_two_machines_recursive.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Two machines, both canmount on, simple switch":    {def: "m_two_machines_both_canmount_on.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Two machines, persistent":                         {def: "m_two_machines_with_persistent.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Two machines, separate user dataset":              {def: "m_two_machines_with_different_userdata.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Two machines, same user dataset":                  {def: "m_two_machines_with_same_userdata.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Two machines, separate boot":                      {def: "m_two_machines_with_separate_boot.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Reset boot on first main dataset, without suffix": {def: "m_main_dataset_without_suffix_and_clone.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu")},

		// Clone switch
		"Clone, keep main active":                                              {def: "m_clone_simple.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_1234"), isNoOp: true},
		"Clone, simple switch":                                                 {def: "m_clone_simple.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Clone, with children":                                                 {def: "m_clone_with_children.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Clone, both canmount on, simple switch":                               {def: "m_clone_both_canmount_on.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Clone, persistent":                                                    {def: "m_clone_with_persistent.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Clone, separate user dataset":                                         {def: "m_clone_with_userdata.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Clone, separate user dataset with children":                           {def: "m_clone_with_userdata_with_children.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Clone, separate user dataset with children manually created":          {def: "m_clone_with_userdata_with_children_manually_created.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Clone, separate reverted user dataset":                                {def: "m_clone_with_userdata.yaml", cmdline: generateCmdLineWithRevert("rpool/ROOT/ubuntu_5678")},
		"Clone, separate reverted user dataset with children":                  {def: "m_clone_with_userdata_with_children.yaml", cmdline: generateCmdLineWithRevert("rpool/ROOT/ubuntu_5678")},
		"Clone, separate reverted user dataset with children manually created": {def: "m_clone_with_userdata_with_children_manually_created.yaml", cmdline: generateCmdLineWithRevert("rpool/ROOT/ubuntu_5678")},
		"Clone, separate boot":                                                 {def: "m_clone_with_separate_boot.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Clone, separate boot with children":                                   {def: "m_clone_with_separate_boot_with_children.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Clone, separate boot with children manually created":                  {def: "m_clone_with_separate_boot_with_children_manually_created.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},

		// Reverting userdata
		"Reverting userdata, with children": {def: "m_clone_with_userdata_with_children_reverting.yaml",
			cmdline:        generateCmdLineWithRevert("rpool/ROOT/ubuntu_1234@snap1"),
			mountedDataset: "rpool/ROOT/ubuntu_4242"},

		// Booting on snapshot on real machines
		"Desktop revert on snapshot": {def: "m_layout1_machines_with_snapshots_clones_reverting.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678@snap3"), mountedDataset: "rpool/ROOT/ubuntu_4242"},
		"Desktop revert on snapshot with userdata revert": {def: "m_layout1_machines_with_snapshots_clones_reverting.yaml",
			cmdline:        generateCmdLineWithRevert("rpool/ROOT/ubuntu_5678@snap3"),
			mountedDataset: "rpool/ROOT/ubuntu_4242"},
		"Server revert on snapshot": {def: "m_layout2_machines_with_snapshots_clones_reverting.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678@snap3"), mountedDataset: "rpool/ROOT/ubuntu_4242"},
		"Server revert on snapshot with userdata revert": {def: "m_layout2_machines_with_snapshots_clones_reverting.yaml",
			cmdline:        generateCmdLineWithRevert("rpool/ROOT/ubuntu_5678@snap3"),
			mountedDataset: "rpool/ROOT/ubuntu_4242"},

		// Error cases
		"No booted state found does nothing":       {def: "m_layout1_machines_with_snapshots_clones_reverting.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678@snap3"), isNoOp: true},
		"SetProperty fails":                        {def: "m_layout1_machines_with_snapshots_clones_reverting.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678@snap3"), mountedDataset: "rpool/ROOT/ubuntu_4242", setPropertyErr: true, wantErr: true},
		"SetProperty fails with revert":            {def: "m_layout1_machines_with_snapshots_clones_reverting.yaml", cmdline: generateCmdLineWithRevert("rpool/ROOT/ubuntu_5678@snap3"), mountedDataset: "rpool/ROOT/ubuntu_4242", setPropertyErr: true, wantErr: true},
		"Scan fails":                               {def: "m_layout1_machines_with_snapshots_clones_reverting.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678@snap3"), mountedDataset: "rpool/ROOT/ubuntu_4242", scanErr: true, wantErr: true},
		"Clone fails":                              {def: "m_layout1_machines_with_snapshots_clones_reverting.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678@snap3"), mountedDataset: "rpool/ROOT/ubuntu_4242", cloneErr: true, wantErr: true},
		"Revert on created dataset without suffix": {def: "m_new_dataset_without_suffix_and_clone.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678@snap1"), mountedDataset: "rpool/ROOT/ubuntu", wantErr: true},
	}

	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir, cleanup := testutils.TempDir(t)
			defer cleanup()
			libzfs := testutils.GetMockZFS(t)
			fPools := testutils.NewFakePools(t, filepath.Join("testdata", tc.def), testutils.WithLibZFS(libzfs))
			defer fPools.Create(dir)()

			lzfs := libzfs.(*mock.LibZFS)
			if tc.mountedDataset != "" {
				lzfs.SetDatasetAsMounted(tc.mountedDataset, true)
			}

			ms, err := machines.New(context.Background(), tc.cmdline, machines.WithLibZFS(libzfs))
			if err != nil {
				t.Error("expected success but got an error scanning for machines", err)
			}
			initMachines := ms.CopyForTests(t)

			lzfs.ErrOnClone(tc.cloneErr)
			lzfs.ErrOnScan(tc.scanErr)
			lzfs.ErrOnSetProperty(tc.setPropertyErr)

			hasChanged, err := ms.EnsureBoot(context.Background())
			if err != nil {
				if !tc.wantErr {
					t.Fatalf("expected no error but got: %v", err)
				}
				return
			}
			if err == nil && tc.wantErr {
				t.Fatal("expected an error but got none")
			}

			if tc.isNoOp {
				assert.False(t, hasChanged, "expected signaling no change in commit but had some")
				assertMachinesEquals(t, initMachines, ms)
			} else {
				assert.True(t, hasChanged, "expected signalling change in commit but told none")
				assertMachinesToGolden(t, ms)
				assertMachinesNotEquals(t, initMachines, ms)
			}

			machinesAfterRescan, err := machines.New(context.Background(), tc.cmdline, machines.WithLibZFS(libzfs))
			if err != nil {
				t.Error("expected success but got an error scanning for machines", err)
			}
			assertMachinesEquals(t, machinesAfterRescan, ms)
		})
	}
}

func TestIdempotentBoot(t *testing.T) {
	t.Parallel()
	dir, cleanup := testutils.TempDir(t)
	defer cleanup()

	libzfs := testutils.GetMockZFS(t)
	fPools := testutils.NewFakePools(t, filepath.Join("testdata", "m_layout2_machines_with_snapshots_clones_reverting.yaml"), testutils.WithLibZFS(libzfs))
	defer fPools.Create(dir)()

	ms, err := machines.New(context.Background(), generateCmdLineWithRevert("rpool/ROOT/ubuntu_5678"), machines.WithLibZFS(libzfs))
	if err != nil {
		t.Error("expected success but got an error at first scan on machines", err)
	}

	hasChanged, err := ms.EnsureBoot(context.Background())
	if err != nil {
		t.Fatalf("expected no error but got: %v", err)
	}

	assert.True(t, hasChanged, "expected first boot to signal a change, but got false")
	msAfterEnsureBoot := ms.CopyForTests(t)

	hasChanged, err = ms.EnsureBoot(context.Background())
	if err != nil {
		t.Fatalf("expected no error but got: %v", err)
	}
	assert.False(t, hasChanged, "expected second boot to signal no change, but got true")

	assertMachinesEquals(t, msAfterEnsureBoot, ms)
}

// TODO: not really idempotent, but should untag datasets that are tagged with destination datasets, maybe even destroy if it's the only one?
// check what happens in case of daemon-reload… Maybe should be a no-op if mounted (and we should simulate here mounting user datasets)
// Destroy if no LastUsed for user datasets?
// Destroy system non boot dataset?
func TestIdempotentBootSnapshotSuccess(t *testing.T) {
	t.Parallel()
	dir, cleanup := testutils.TempDir(t)
	defer cleanup()

	libzfs := testutils.GetMockZFS(t)
	fPools := testutils.NewFakePools(t, filepath.Join("testdata", "m_layout2_machines_with_snapshots_clones_reverting.yaml"), testutils.WithLibZFS(libzfs))
	defer fPools.Create(dir)()

	lzfs := libzfs.(*mock.LibZFS)
	lzfs.SetDatasetAsMounted("rpool/ROOT/ubuntu_4242", true)

	ms, err := machines.New(context.Background(), generateCmdLineWithRevert("rpool/ROOT/ubuntu_5678@snap3"), machines.WithLibZFS(libzfs))
	if err != nil {
		t.Error("expected success but got an error at first scan on machines", err)
	}

	hasChanged, err := ms.EnsureBoot(context.Background())
	if err != nil {
		t.Fatalf("expected no error but got: %v", err)
	}
	assert.True(t, hasChanged, "expected first boot to signal a change, but got false")

	hasChanged, err = ms.Commit(context.Background())
	if err != nil {
		t.Fatal("Commit failed:", err)
	}
	assert.True(t, hasChanged, "expected first commit to signal a change, but got false")
	msAfterCommit := ms.CopyForTests(t)

	hasChanged, err = ms.EnsureBoot(context.Background())
	if err != nil {
		t.Fatalf("expected no error but got: %v", err)
	}
	assert.False(t, hasChanged, "expected second boot to signal no change, but got true")

	assertMachinesEquals(t, msAfterCommit, ms)
}

func TestIdempotentBootSnapshotBeforeCommit(t *testing.T) {
	t.Parallel()
	dir, cleanup := testutils.TempDir(t)
	defer cleanup()

	libzfs := testutils.GetMockZFS(t)
	fPools := testutils.NewFakePools(t, filepath.Join("testdata", "m_layout2_machines_with_snapshots_clones_reverting.yaml"), testutils.WithLibZFS(libzfs))
	defer fPools.Create(dir)()

	lzfs := libzfs.(*mock.LibZFS)
	lzfs.SetDatasetAsMounted("rpool/ROOT/ubuntu_4242", true)

	ms, err := machines.New(context.Background(), generateCmdLineWithRevert("rpool/ROOT/ubuntu_5678@snap3"), machines.WithLibZFS(libzfs))
	if err != nil {
		t.Error("expected success but got an error at first scan on machines", err)
	}

	hasChanged, err := ms.EnsureBoot(context.Background())
	if err != nil {
		t.Fatalf("expected no error but got: %v", err)
	}
	assert.True(t, hasChanged, "expected first boot to signal a change, but got false")

	msAfterEnsureBoot := ms.CopyForTests(t)

	hasChanged, err = ms.EnsureBoot(context.Background())
	if err != nil {
		t.Fatalf("expected no error but got: %v", err)
	}
	assert.False(t, hasChanged, "expected second boot to signal no change, but got true")

	assertMachinesEquals(t, msAfterEnsureBoot, ms)
}

func TestCommit(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		def     string
		cmdline string

		scanErr        bool
		setPropertyErr bool
		promoteErr     bool

		wantNoChange bool
		wantErr      bool
		isNoOp       bool
	}{
		"One machine, commit one clone":                      {def: "d_one_machine_with_clone_to_promote.yaml", cmdline: generateCmdLine("rpool/clone")},
		"One machine, commit current":                        {def: "d_one_machine_with_clone_dataset.yaml", cmdline: generateCmdLine("rpool/main")},
		"One machine, update LastUsed but same kernel":       {def: "d_one_machine_with_clone_dataset.yaml", cmdline: generateCmdLine("rpool/main BOOT_IMAGE=vmlinuz-5.2.0-8-generic"), wantNoChange: true},
		"One machine, update LastUsed and Kernel":            {def: "d_one_machine_with_clone_dataset.yaml", cmdline: generateCmdLine("rpool/main BOOT_IMAGE=vmlinuz-9.9.9-9-generic")},
		"One machine, set LastUsed and Kernel basename":      {def: "d_one_machine_with_clone_to_promote.yaml", cmdline: generateCmdLine("rpool/clone BOOT_IMAGE=/boot/vmlinuz-9.9.9-9-generic")},
		"One machine, Kernel basename with already basename": {def: "d_one_machine_with_clone_to_promote.yaml", cmdline: generateCmdLine("rpool/clone BOOT_IMAGE=vmlinuz-9.9.9-9-generic")},
		"One machine non zsys":                               {def: "d_one_machine_one_dataset_non_zsys.yaml", cmdline: generateCmdLine("rpool"), isNoOp: true, wantNoChange: true},
		"One machine no match":                               {def: "d_one_machine_one_dataset.yaml", cmdline: generateCmdLine("rpoolfake"), isNoOp: true, wantNoChange: true},

		"One machine with children":                                       {def: "m_clone_with_children_to_promote.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"One machine with children, LastUsed and kernel basename on root": {def: "m_clone_with_children_to_promote.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678 BOOT_IMAGE=/boot/vmlinuz-9.9.9-9-generic")},
		"Without suffix": {def: "m_main_dataset_without_suffix_and_clone_to_promote.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu")},

		"Separate user dataset, no user revert":                                {def: "m_clone_with_userdata_to_promote_no_user_revert.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Separate user dataset with children, no user revert":                  {def: "m_clone_with_userdata_with_children_to_promote_no_user_revert.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Separate user dataset with children manually created, no user revert": {def: "m_clone_with_userdata_with_children_manually_created_to_promote_no_user_revert.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Separate user dataset, user revert":                                   {def: "m_clone_with_userdata_to_promote_user_revert.yaml", cmdline: generateCmdLineWithRevert("rpool/ROOT/ubuntu_5678")},
		"Separate user dataset with children, user revert":                     {def: "m_clone_with_userdata_with_children_to_promote_user_revert.yaml", cmdline: generateCmdLineWithRevert("rpool/ROOT/ubuntu_5678")},
		"Separate user dataset with children manually created, user revert":    {def: "m_clone_with_userdata_with_children_manually_created_to_promote_user_revert.yaml", cmdline: generateCmdLineWithRevert("rpool/ROOT/ubuntu_5678")},

		"Separate boot":                                {def: "m_clone_with_separate_boot_to_promote.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Separate boot with children":                  {def: "m_clone_with_separate_boot_with_children_to_promote.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Separate boot with children manually created": {def: "m_clone_with_separate_boot_with_children_manually_created_to_promote.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},

		// Real machines
		"Desktop without user revert": {def: "m_layout1_machines_with_snapshots_clones_no_user_revert.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_9876")},
		"Desktop with user revert":    {def: "m_layout1_machines_with_snapshots_clones_user_revert.yaml", cmdline: generateCmdLineWithRevert("rpool/ROOT/ubuntu_9876")},
		"Server without user revert":  {def: "m_layout2_machines_with_snapshots_clones_no_user_revert.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_9876")},
		"Server with user revert":     {def: "m_layout2_machines_with_snapshots_clones_user_revert.yaml", cmdline: generateCmdLineWithRevert("rpool/ROOT/ubuntu_9876")},

		// Error cases
		"SetProperty fails (first)":  {def: "m_clone_with_userdata_with_children_to_promote_no_user_revert.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678"), setPropertyErr: true, wantErr: true},
		"SetProperty fails (second)": {def: "m_clone_with_userdata_to_promote_no_user_revert.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678"), setPropertyErr: true, wantErr: true},
		"Promote fails":              {def: "d_one_machine_with_clone_dataset.yaml", cmdline: generateCmdLine("rpool/clone"), promoteErr: true, wantErr: true},
		"Promote userdata fails":     {def: "m_clone_with_userdata_to_promote_user_revert.yaml", cmdline: generateCmdLineWithRevert("rpool/ROOT/ubuntu_5678"), promoteErr: true, wantErr: true},
		"Scan fails":                 {def: "d_one_machine_with_clone_dataset.yaml", cmdline: generateCmdLine("rpool/main"), scanErr: true, wantErr: true},
	}
	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir, cleanup := testutils.TempDir(t)
			defer cleanup()
			libzfs := testutils.GetMockZFS(t)
			fPools := testutils.NewFakePools(t, filepath.Join("testdata", tc.def), testutils.WithLibZFS(libzfs))
			defer fPools.Create(dir)()

			ms, err := machines.New(context.Background(), tc.cmdline, machines.WithLibZFS(libzfs))
			if err != nil {
				t.Error("expected success but got an error scanning for machines", err)
			}
			lzfs := libzfs.(*mock.LibZFS)

			lzfs.ErrOnScan(tc.scanErr)
			lzfs.ErrOnSetProperty(tc.setPropertyErr)
			lzfs.ErrOnPromote(tc.promoteErr)
			lzfs.ForceLastUsedTime(true)
			initMachines := ms.CopyForTests(t)

			hasChanged, err := ms.Commit(context.Background())
			if err != nil {
				if !tc.wantErr {
					t.Fatalf("expected no error but got: %v", err)
				}
				return
			}
			if err == nil && tc.wantErr {
				t.Fatal("expected an error but got none")
			}

			assert.Equal(t, !tc.wantNoChange, hasChanged, "change returned signal")
			if tc.isNoOp {
				assertMachinesEquals(t, initMachines, ms)
			} else {
				assertMachinesToGolden(t, ms)
				assertMachinesNotEquals(t, initMachines, ms)
			}

			machinesAfterRescan, err := machines.New(context.Background(), tc.cmdline, machines.WithLibZFS(libzfs))
			if err != nil {
				t.Error("expected success but got an error scanning for machines", err)
			}
			assertMachinesEquals(t, machinesAfterRescan, ms)
		})
	}
}

func TestIdempotentCommit(t *testing.T) {
	t.Parallel()
	dir, cleanup := testutils.TempDir(t)
	defer cleanup()

	libzfs := testutils.GetMockZFS(t)
	fPools := testutils.NewFakePools(t, filepath.Join("testdata", "m_layout2_machines_with_snapshots_clones_no_user_revert.yaml"), testutils.WithLibZFS(libzfs))
	defer fPools.Create(dir)()

	lzfs := libzfs.(*mock.LibZFS)
	lzfs.SetDatasetAsMounted("rpool/ROOT/ubuntu_9876", true)
	lzfs.ForceLastUsedTime(true)

	ms, err := machines.New(context.Background(), generateCmdLine("rpool/ROOT/ubuntu_9876"), machines.WithLibZFS(libzfs))
	if err != nil {
		t.Error("expected success but got an error at first scan on machines", err)
	}

	hasChanged, err := ms.Commit(context.Background())
	if err != nil {
		t.Fatal("first commit failed:", err)
	}
	assert.True(t, hasChanged, "expected first commit to signal a change, but got false")

	msAfterCommit := ms.CopyForTests(t)

	hasChanged, err = ms.Commit(context.Background())
	if err != nil {
		t.Fatal("second commit failed:", err)
	}
	assert.False(t, hasChanged, "expected second commit to signal no change, but got true")

	assertMachinesEquals(t, msAfterCommit, ms)
}

func TestUpdateLastUsed(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		def     string
		cmdline string

		setPropertyErr bool

		wantErr bool
		isNoOp  bool
	}{
		"Update on system and user datasets":        {def: "m_with_userdata.yaml"},
		"Update on correct current machine":         {def: "m_two_machines_with_different_userdata.yaml"},
		"Update only current user and system state": {def: "state_snapshot_with_userdata_01.yaml"},

		"Doesn't update on non zsys machine":   {def: "m_with_userdata_no_zsys.yaml", isNoOp: true},
		"No current machine":                   {def: "m_with_userdata.yaml", cmdline: "doesntexist", isNoOp: true},
		"Error on setting property is a no op": {def: "m_with_userdata.yaml", setPropertyErr: true, wantErr: true, isNoOp: true},
	}

	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tc.cmdline = getDefaultValue(tc.cmdline, generateCmdLine("rpool/ROOT/ubuntu_1234"))

			dir, cleanup := testutils.TempDir(t)
			defer cleanup()
			libzfs := testutils.GetMockZFS(t)
			fPools := testutils.NewFakePools(t, filepath.Join("testdata", tc.def), testutils.WithLibZFS(libzfs))
			defer fPools.Create(dir)()

			lzfs := libzfs.(*mock.LibZFS)
			lzfs.ForceLastUsedTime(true)

			ms, err := machines.New(context.Background(), tc.cmdline, machines.WithLibZFS(libzfs))
			if err != nil {
				t.Error("expected success but got an error scanning for machines", err)
			}

			initMachines := ms.CopyForTests(t)

			lzfs.ErrOnSetProperty(tc.setPropertyErr)

			err = ms.UpdateLastUsed(context.Background())
			if err != nil {
				if !tc.wantErr {
					t.Fatalf("expected no error but got: %v", err)
				}
				return
			}
			if err == nil && tc.wantErr {
				t.Fatal("expected an error but got none")
			}

			if tc.isNoOp {
				assertMachinesEquals(t, initMachines, ms)
			} else {
				assertMachinesToGolden(t, ms)
				assertMachinesNotEquals(t, initMachines, ms)
			}

			machinesAfterRescan, err := machines.New(context.Background(), tc.cmdline, machines.WithLibZFS(libzfs))
			if err != nil {
				t.Error("expected success but got an error scanning for machines", err)
			}
			assertMachinesEquals(t, machinesAfterRescan, ms)
		})
	}
}

func TestCreateUserData(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		def      string
		user     string
		homePath string
		cmdline  string

		setPropertyErr bool
		createErr      bool
		scanErr        bool

		wantErr bool
		isNoOp  bool
	}{
		"One machine add user dataset":                  {def: "m_with_userdata.yaml"},
		"One machine add user dataset without userdata": {def: "m_without_userdata.yaml"},
		"One machine with no user, only userdata":       {def: "m_with_userdata_only.yaml"},
		"No attached userdata":                          {def: "m_no_attached_userdata_first_pool.yaml"},

		// Second pool cases
		"User dataset on other pool":                       {def: "m_with_userdata_on_other_pool.yaml"},
		"User dataset with no user on other pool":          {def: "m_with_userdata_only_on_other_pool.yaml"},
		"Prefer system pool for userdata":                  {def: "m_without_userdata_prefer_system_pool.yaml"},
		"Prefer system pool (try other pool) for userdata": {def: "m_without_userdata_prefer_system_pool.yaml", cmdline: generateCmdLine("rpool2/ROOT/ubuntu_1234")},
		"No attached userdata on second pool":              {def: "m_no_attached_userdata_second_pool.yaml"},

		// User or home edge cases
		"No user set":                                           {def: "m_with_userdata.yaml", user: "[empty]", wantErr: true, isNoOp: true},
		"No home path set":                                      {def: "m_with_userdata.yaml", homePath: "[empty]", wantErr: true, isNoOp: true},
		"User already exists on this machine":                   {def: "m_with_userdata.yaml", user: "user1"},
		"Target directory already exists and match user":        {def: "m_with_userdata.yaml", user: "user1", homePath: "/home/user1", isNoOp: true},
		"Target directory already exists and don't match user":  {def: "m_with_userdata.yaml", homePath: "/home/user1", wantErr: true, isNoOp: true},
		"Set Property when user already exists on this machine": {def: "m_with_userdata.yaml", setPropertyErr: true, user: "user1", wantErr: true, isNoOp: true},
		"Scan when user already exists fails":                   {def: "m_with_userdata.yaml", scanErr: true, user: "user1", wantErr: true, isNoOp: true},

		// Error cases
		"System not zsys":                       {def: "m_with_userdata_no_zsys.yaml", wantErr: true, isNoOp: true},
		"Create user dataset fails":             {def: "m_with_userdata.yaml", createErr: true, wantErr: true, isNoOp: true},
		"Create user dataset container fails":   {def: "m_without_userdata.yaml", createErr: true, wantErr: true, isNoOp: true},
		"System bootfs property fails":          {def: "m_with_userdata.yaml", setPropertyErr: true, wantErr: true, isNoOp: true},
		"Scan for user dataset container fails": {def: "m_without_userdata.yaml", scanErr: true, wantErr: true, isNoOp: true},
		"Final scan triggers error":             {def: "m_with_userdata.yaml", scanErr: true, wantErr: true, isNoOp: true},
	}

	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tc.cmdline = getDefaultValue(tc.cmdline, generateCmdLine("rpool/ROOT/ubuntu_1234"))

			dir, cleanup := testutils.TempDir(t)
			defer cleanup()
			libzfs := testutils.GetMockZFS(t)
			fPools := testutils.NewFakePools(t, filepath.Join("testdata", tc.def), testutils.WithLibZFS(libzfs))
			defer fPools.Create(dir)()

			lzfs := libzfs.(*mock.LibZFS)
			lzfs.ForceLastUsedTime(true)

			ms, err := machines.New(context.Background(), tc.cmdline, machines.WithLibZFS(libzfs))
			if err != nil {
				t.Error("expected success but got an error scanning for machines", err)
			}

			initMachines := ms.CopyForTests(t)

			lzfs.ErrOnCreate(tc.createErr)
			lzfs.ErrOnScan(tc.scanErr)
			lzfs.ErrOnSetProperty(tc.setPropertyErr)

			err = ms.CreateUserData(context.Background(), getDefaultValue(tc.user, "userfoo"), getDefaultValue(tc.homePath, "/home/foo"))
			if err != nil {
				if !tc.wantErr {
					t.Fatalf("expected no error but got: %v", err)
				}
				return
			}
			if err == nil && tc.wantErr {
				t.Fatal("expected an error but got none")
			}

			if tc.isNoOp {
				assertMachinesEquals(t, initMachines, ms)
			} else {
				assertMachinesToGolden(t, ms)
				assertMachinesNotEquals(t, initMachines, ms)
			}

			machinesAfterRescan, err := machines.New(context.Background(), tc.cmdline, machines.WithLibZFS(libzfs))
			if err != nil {
				t.Error("expected success but got an error scanning for machines", err)
			}
			assertMachinesEquals(t, machinesAfterRescan, ms)
		})
	}
}

func TestDissociateUser(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		def     string
		user    string
		cmdline string

		setPropertyErr bool
		scanErr        bool

		wantErr bool
		isNoOp  bool
	}{
		"Dissociate user": {def: "m_with_userdata.yaml"},

		"Dissociate user but keep snapshot attached":         {def: "m_with_userdata_dataset_and_snapshot_attached.yaml"},
		"Dissociate user with no associated snapshot":        {def: "m_with_userdata_user_snapshot.yaml"},
		"Dissociate user with children datasets (inherited)": {def: "m_with_userdata_children_on_user.yaml"},
		"Dissociate user with children datasets (local)":     {def: "m_with_userdata_clone_attached.yaml"},
		"Dissociate user having shared states":               {def: "m_two_machines_with_same_userdata.yaml"},

		"User has no state associated with current machine": {def: "m_with_userdata.yaml", user: "doesntexist", wantErr: true},
		"SetProperty fails":          {def: "m_with_userdata.yaml", setPropertyErr: true, wantErr: true},
		"Scanning fails":             {def: "m_with_userdata.yaml", scanErr: true, wantErr: true},
		"Current machine isn’t zsys": {def: "m_with_userdata.yaml", cmdline: "foo", wantErr: true},
	}

	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tc.cmdline = getDefaultValue(tc.cmdline, generateCmdLine("rpool/ROOT/ubuntu_1234"))

			dir, cleanup := testutils.TempDir(t)
			defer cleanup()
			libzfs := testutils.GetMockZFS(t)
			fPools := testutils.NewFakePools(t, filepath.Join("testdata", tc.def), testutils.WithLibZFS(libzfs))
			defer fPools.Create(dir)()

			lzfs := libzfs.(*mock.LibZFS)
			lzfs.ForceLastUsedTime(true)

			ms, err := machines.New(context.Background(), tc.cmdline, machines.WithLibZFS(libzfs))
			if err != nil {
				t.Error("expected success but got an error scanning for machines", err)
			}

			initMachines := ms.CopyForTests(t)

			lzfs.ErrOnScan(tc.scanErr)
			lzfs.ErrOnSetProperty(tc.setPropertyErr)

			err = ms.DissociateUser(context.Background(), getDefaultValue(tc.user, "user1"))
			if err != nil {
				if !tc.wantErr {
					t.Fatalf("expected no error but got: %v", err)
				}
				return
			}
			if err == nil && tc.wantErr {
				t.Fatal("expected an error but got none")
			}

			if tc.isNoOp {
				assertMachinesEquals(t, initMachines, ms)
			} else {
				assertMachinesToGolden(t, ms)
				assertMachinesNotEquals(t, initMachines, ms)
			}

			machinesAfterRescan, err := machines.New(context.Background(), tc.cmdline, machines.WithLibZFS(libzfs))
			if err != nil {
				t.Error("expected success but got an error scanning for machines", err)
			}
			assertMachinesEquals(t, machinesAfterRescan, ms)
		})
	}
}

func TestCurrentIsZsys(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		cmdline string

		wantIsZsys bool
	}{
		"Zsys machine is current":         {cmdline: generateCmdLine("rpool"), wantIsZsys: true},
		"Zfs non zsys machine is current": {cmdline: generateCmdLine("rpool2"), wantIsZsys: false},
		"No current machine":              {cmdline: generateCmdLine("something that doesn’t match"), wantIsZsys: false},
	}

	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir, cleanup := testutils.TempDir(t)
			defer cleanup()

			libzfs := testutils.GetMockZFS(t)
			fPools := testutils.NewFakePools(t, filepath.Join("testdata", "d_two_machines_one_zsys_one_non_zsys.yaml"), testutils.WithLibZFS(libzfs))
			defer fPools.Create(dir)()

			/*if tc.mountedDataset != "" {
				lzfs := libzfs.(*mock.LibZFS)
				lzfs.SetDatasetAsMounted(tc.mountedDataset, true)
			}*/

			ms, err := machines.New(context.Background(), tc.cmdline, machines.WithLibZFS(libzfs))
			if err != nil {
				t.Error("expected success but got an error scanning for machines", err)
			}

			assert.Equal(t, tc.wantIsZsys, ms.CurrentIsZsys(), "Expected current is zsys returned value")
		})
	}
}

func TestChangeHomeOnUserData(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		def     string
		home    string
		newHome string

		setPropertyErr bool
		scanErr        bool

		wantErr bool
		isNoOp  bool
	}{
		"Rename home": {def: "m_with_userdata.yaml"},

		// Argument or matching issue
		"Home doesn't match": {def: "m_with_userdata.yaml", home: "/home/userabcd", wantErr: true, isNoOp: true},
		"System not zsys":    {def: "m_with_userdata_no_zsys.yaml", wantErr: true, isNoOp: true},
		"Old home empty":     {def: "m_with_userdata.yaml", home: "[empty]", wantErr: true, isNoOp: true},
		"New home empty":     {def: "m_with_userdata.yaml", newHome: "[empty]", wantErr: true, isNoOp: true},

		// Errors
		"Set property fails":            {def: "m_with_userdata.yaml", setPropertyErr: true, wantErr: true, isNoOp: true},
		"Scan fails does trigger error": {def: "m_with_userdata.yaml", scanErr: true, wantErr: true, isNoOp: true},
	}

	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir, cleanup := testutils.TempDir(t)
			defer cleanup()
			libzfs := testutils.GetMockZFS(t)
			fPools := testutils.NewFakePools(t, filepath.Join("testdata", tc.def), testutils.WithLibZFS(libzfs))
			defer fPools.Create(dir)()

			lzfs := libzfs.(*mock.LibZFS)
			lzfs.ForceLastUsedTime(true)

			ms, err := machines.New(context.Background(), generateCmdLine("rpool/ROOT/ubuntu_1234"), machines.WithLibZFS(libzfs))
			if err != nil {
				t.Error("expected success but got an error scanning for machines", err)
			}

			initMachines := ms.CopyForTests(t)

			lzfs.ErrOnScan(tc.scanErr)
			lzfs.ErrOnSetProperty(tc.setPropertyErr)

			err = ms.ChangeHomeOnUserData(context.Background(), getDefaultValue(tc.home, "/home/user1"), getDefaultValue(tc.newHome, "/home/foo"))
			if err != nil {
				if !tc.wantErr {
					t.Fatalf("expected no error but got: %v", err)
				}
				return
			}
			if err == nil && tc.wantErr {
				t.Fatal("expected an error but got none")
			}

			if tc.isNoOp {
				assertMachinesEquals(t, initMachines, ms)
			} else {
				assertMachinesToGolden(t, ms)
				assertMachinesNotEquals(t, initMachines, ms)
			}

			// finale rescan uneeded if last one failed
			machinesAfterRescan, err := machines.New(context.Background(), generateCmdLine("rpool/ROOT/ubuntu_1234"), machines.WithLibZFS(libzfs))
			if err != nil {
				t.Error("expected success but got an error scanning for machines", err)
			}
			assertMachinesEquals(t, machinesAfterRescan, ms)
		})
	}
}

func TestCreateSystemSnapshot(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		def          string
		cmdline      string
		snapshotName string

		setCapOnPool string
		capValue     string

		wantErr bool
		isNoOp  bool
	}{
		"Take one snapshot":       {def: "m_with_userdata.yaml"},
		"Give a name to snapshot": {def: "m_with_userdata.yaml", snapshotName: "my_snapshot"},

		"Children on system datasets": {def: "m_with_userdata_children_on_system.yaml"},
		"Children on user datasets":   {def: "m_with_userdata_children_on_user.yaml"},
		"Children on user datasets with one child non associated with current machine": {def: "m_with_userdata_child_associated_one_state.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_9999")},

		"No associated userdata": {def: "d_one_machine_with_children.yaml", cmdline: generateCmdLine("rpool")},

		// Free space handling
		"Not enough free space on system pool":                {def: "m_with_userdata_on_other_pool.yaml", setCapOnPool: "rpool", capValue: "99", wantErr: true},
		"Not enough free space on user pool":                  {def: "m_with_userdata_on_other_pool.yaml", setCapOnPool: "rpool2", capValue: "99", wantErr: true},
		"Capacity is invalid":                                 {def: "m_with_userdata_on_other_pool.yaml", setCapOnPool: "rpool", capValue: "NaN", wantErr: true},
		"Take snapshot, not enough free space on other pools": {def: "m_without_userdata_prefer_system_pool.yaml", setCapOnPool: "rpool2", capValue: "99"},

		// error cases with snapshot exists on root. on userdataset. on system child. on user child
		"Error on existing snapshot on system root":  {def: "m_with_userdata_and_multiple_snapshots.yaml", snapshotName: "system_root_snapshot", wantErr: true, isNoOp: true},
		"Error on existing snapshot on system child": {def: "m_with_userdata_and_multiple_snapshots.yaml", snapshotName: "system_child_snapshot", wantErr: true, isNoOp: true},
		"Error on existing snapshot on user root":    {def: "m_with_userdata_and_multiple_snapshots.yaml", snapshotName: "user_root_snapshot", wantErr: true, isNoOp: true},
		"Error on existing snapshot on user child":   {def: "m_with_userdata_and_multiple_snapshots.yaml", snapshotName: "user_child_snapshot", wantErr: true, isNoOp: true},

		"Non zsys":   {def: "m_with_userdata_no_zsys.yaml", wantErr: true, isNoOp: true},
		"No machine": {def: "m_with_userdata_no_zsys.yaml", cmdline: generateCmdLine("rpool/ROOT/nomachine"), wantErr: true, isNoOp: true},
	}

	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir, cleanup := testutils.TempDir(t)
			defer cleanup()
			libzfs := testutils.GetMockZFS(t)
			fPools := testutils.NewFakePools(t, filepath.Join("testdata", tc.def), testutils.WithLibZFS(libzfs))
			defer fPools.Create(dir)()

			if tc.cmdline == "" {
				tc.cmdline = generateCmdLine("rpool/ROOT/ubuntu_1234")
			}

			ms, err := machines.New(context.Background(), tc.cmdline, machines.WithLibZFS(libzfs))
			if err != nil {
				t.Error("expected success but got an error scanning for machines", err)
			}
			lzfs := libzfs.(*mock.LibZFS)

			lzfs.ForceLastUsedTime(true)
			if tc.setCapOnPool != "" {
				lzfs.SetPoolCapacity(tc.setCapOnPool, tc.capValue)
			}

			initMachines := ms.CopyForTests(t)

			snapshotName, err := ms.CreateSystemSnapshot(context.Background(), tc.snapshotName)
			if err != nil {
				if !tc.wantErr {
					t.Fatalf("expected no error but got: %v", err)
				}
				return
			}
			if err == nil && tc.wantErr {
				t.Fatal("expected an error but got none")
			}

			if tc.snapshotName != "" {
				if snapshotName != tc.snapshotName {
					t.Errorf("provided snapshotname isn't the one used. Want: %s, got: %s", tc.snapshotName, snapshotName)
				}
			} else {
				if !strings.HasPrefix(snapshotName, machines.AutomatedSnapshotPrefix) {
					t.Errorf("generated snapshotname should start with %s, but got: %s", machines.AutomatedSnapshotPrefix, snapshotName)
				}
			}

			if tc.isNoOp {
				assertMachinesEquals(t, initMachines, ms)
			} else {
				assertMachinesToGolden(t, ms)
				assertMachinesNotEquals(t, initMachines, ms)
			}

			// finale rescan uneeded if last one failed
			machinesAfterRescan, err := machines.New(context.Background(), tc.cmdline, machines.WithLibZFS(libzfs))
			if err != nil {
				t.Error("expected success but got an error scanning for machines", err)
			}
			assertMachinesEquals(t, machinesAfterRescan, ms)
		})
	}
}

func TestCreateUserSnapshot(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		def          string
		cmdline      string
		snapshotName string
		userName     string

		setCapOnPool string
		capValue     string

		wantErr bool
		isNoOp  bool
	}{
		"Take one snapshot":                                   {def: "m_with_userdata.yaml"},
		"Give a name to snapshot":                             {def: "m_with_userdata.yaml", snapshotName: "my_snapshot"},
		"Take one snapshot on a machine with other snapshots": {def: "m_snapshot_with_userdata.yaml"},

		"Children on user datasets": {def: "m_with_userdata_children_on_user.yaml"},
		"Children on user datasets with one child non associated with current machine": {def: "m_with_userdata_child_associated_one_state.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_9999")},

		// Space handling
		"Not enough free space on user pool":                       {def: "m_with_userdata_on_other_pool.yaml", setCapOnPool: "rpool2", capValue: "99", wantErr: true},
		"Take user snapshot, not enough free space on other pools": {def: "m_with_userdata_on_other_pool.yaml", setCapOnPool: "rpool", capValue: "99"},
		"Capacity is invalid":                                      {def: "m_with_userdata_on_other_pool.yaml", setCapOnPool: "rpool2", capValue: "NaN", wantErr: true},

		// Error cases with non-existent user, snapshot exists on root, on userdataset, on system child, on user child
		"Error on empty user":                       {def: "m_with_userdata.yaml", userName: "-", wantErr: true, isNoOp: true},
		"Error on non existent user":                {def: "m_with_userdata.yaml", userName: "nonexistent", wantErr: true, isNoOp: true},
		"Error on existing snapshot on system root": {def: "m_with_userdata_and_multiple_snapshots.yaml", snapshotName: "system_root_snapshot", wantErr: true, isNoOp: true},
		"Error on existing snapshot on user root":   {def: "m_with_userdata_and_multiple_snapshots.yaml", snapshotName: "user_root_snapshot", wantErr: true, isNoOp: true},
		"Error on existing snapshot on user child":  {def: "m_with_userdata_and_multiple_snapshots.yaml", snapshotName: "user_child_snapshot", wantErr: true, isNoOp: true},
		// We don’t handle that case: the snapshot is only on a child, so it’s not part of history of this machine, and can’t be automatically reverted to it
		//"Error on existing snapshot on system child": {def: "m_with_userdata_and_multiple_snapshots.yaml", snapshotName: "system_child_snapshot", wantErr: true, isNoOp: true},

		"Non zsys":   {def: "m_with_userdata_no_zsys.yaml", wantErr: true, isNoOp: true},
		"No machine": {def: "m_with_userdata_no_zsys.yaml", cmdline: generateCmdLine("rpool/ROOT/nomachine"), wantErr: true, isNoOp: true},
	}

	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir, cleanup := testutils.TempDir(t)
			defer cleanup()
			libzfs := testutils.GetMockZFS(t)
			fPools := testutils.NewFakePools(t, filepath.Join("testdata", tc.def), testutils.WithLibZFS(libzfs))
			defer fPools.Create(dir)()

			if tc.userName == "" {
				tc.userName = "user1"
			} else if tc.userName == "-" {
				tc.userName = ""
			}
			if tc.cmdline == "" {
				tc.cmdline = generateCmdLine("rpool/ROOT/ubuntu_1234")
			}

			ms, err := machines.New(context.Background(), tc.cmdline, machines.WithLibZFS(libzfs))
			if err != nil {
				t.Error("expected success but got an error scanning for machines", err)
			}
			lzfs := libzfs.(*mock.LibZFS)

			lzfs.ForceLastUsedTime(true)
			if tc.setCapOnPool != "" {
				lzfs.SetPoolCapacity(tc.setCapOnPool, tc.capValue)
			}

			initMachines := ms.CopyForTests(t)

			snapshotName, err := ms.CreateUserSnapshot(context.Background(), tc.userName, tc.snapshotName)
			if err != nil {
				if !tc.wantErr {
					t.Fatalf("expected no error but got: %v", err)
				}
				return
			}
			if err == nil && tc.wantErr {
				t.Fatal("expected an error but got none")
			}

			if tc.snapshotName != "" {
				if snapshotName != tc.snapshotName {
					t.Errorf("provided snapshotname isn't the one used. Want: %s, got: %s", tc.snapshotName, snapshotName)
				}
			} else {
				if !strings.HasPrefix(snapshotName, machines.AutomatedSnapshotPrefix) {
					t.Errorf("generated snapshotname should start with %s, but got: %s", machines.AutomatedSnapshotPrefix, snapshotName)
				}
			}

			if tc.isNoOp {
				assertMachinesEquals(t, initMachines, ms)
			} else {
				assertMachinesToGolden(t, ms)
				assertMachinesNotEquals(t, initMachines, ms)
			}

			// finale rescan uneeded if last one failed
			machinesAfterRescan, err := machines.New(context.Background(), tc.cmdline, machines.WithLibZFS(libzfs))
			if err != nil {
				t.Error("expected success but got an error scanning for machines", err)
			}
			assertMachinesEquals(t, machinesAfterRescan, ms)
		})
	}
}

func TestRemoveState(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		def            string
		currentStateID string
		state          string
		user           string
		force          bool

		destroyErrDS []string

		isNoOp              bool
		wantErr             bool
		wantConfirmationErr bool
	}{
		"Remove system state, one dataset": {def: "m_with_userdata.yaml", state: "rpool/ROOT/ubuntu_1234"},

		// FIXME: miss bpool and bpool/BOOT from golden file
		"Remove system state, complex with boot, children and user datasets": {def: "m_layout1_one_machine.yaml", state: "rpool/ROOT/ubuntu_1234"},
		"Remove one system snapshot only":                                    {def: "state_remove_internal.yaml", state: "rpool/ROOT/ubuntu_1234@snap3"},
		"Removing system state try to delete its snapshots":                  {def: "state_remove.yaml", state: "rpool/ROOT/ubuntu_6789", wantErr: true, wantConfirmationErr: true},
		"Removing system state deletes its snapshots":                        {def: "state_remove.yaml", state: "rpool/ROOT/ubuntu_6789", force: true},

		"Remove system state, with system and users snapshots and clones":         {def: "m_layout1_machines_with_snapshots_clones.yaml", state: "rpool/ROOT/ubuntu_1234", wantErr: true, wantConfirmationErr: true, isNoOp: true},
		"Remove system state, with system and users snapshots and clones, forced": {def: "m_layout1_machines_with_snapshots_clones.yaml", state: "rpool/ROOT/ubuntu_1234", force: true},
		"Remove system state when user datasets are linked to 2 states":           {def: "state_remove_internal.yaml", state: "rpool/ROOT/ubuntu_5678"},
		"Remove system state, with datasets":                                      {def: "state_remove.yaml", state: "rpool/ROOT/ubuntu_1234", wantConfirmationErr: true, wantErr: true, isNoOp: true},
		"Remove system state, with datasets, forced":                              {def: "state_remove.yaml", state: "rpool/ROOT/ubuntu_1234", force: true},

		"Remove user state, one dataset":                       {def: "state_remove.yaml", state: "rpool/USERDATA/user4_clone", user: "user4"},
		"Remove user state, one dataset, no user":              {def: "state_remove.yaml", state: "rpool/USERDATA/user4_clone", wantErr: true, isNoOp: true},
		"Remove user state, one dataset, wrong user":           {def: "state_remove.yaml", state: "rpool/USERDATA/user4_clone", user: "root", wantErr: true, isNoOp: true},
		"Remove user state, with snapshots and clones":         {def: "state_remove.yaml", user: "user6", state: "rpool/USERDATA/user6_clone1", wantErr: true, wantConfirmationErr: true, isNoOp: true},
		"Remove user state, with snapshots and clones, forced": {def: "state_remove.yaml", user: "user6", state: "rpool/USERDATA/user6_clone1", force: true},
		"Remove user state, with datasets":                     {def: "state_remove.yaml", state: "rpool/USERDATA/user5_for-manual-clone@snapuser5", user: "user5", wantErr: true, wantConfirmationErr: true, isNoOp: true},
		"Remove user state, with datasets, forced":             {def: "state_remove.yaml", state: "rpool/USERDATA/user5_for-manual-clone@snapuser5", user: "user5", force: true},
		"Remove user snapshot state":                           {def: "state_remove.yaml", state: "rpool/USERDATA/user1_efgh@snapuser2", user: "user1"},

		"Can’t remove user leaf snapshot linked to system state":                                 {def: "state_remove.yaml", state: "rpool/USERDATA/user1_abcd@snap1", user: "user1", wantErr: true, wantConfirmationErr: true, isNoOp: true},
		"Remove user leaf snapshot linked to system state, forced":                               {def: "state_remove.yaml", state: "rpool/USERDATA/user1_abcd@snap1", user: "user1", force: true},
		"Can’t remove user leaf filesystem state linked to system state":                         {def: "state_remove.yaml", state: "rpool/USERDATA/user3_3333", user: "user3", wantErr: true, wantConfirmationErr: true, isNoOp: true},
		"Remove user leaf filesystem state linked to system state, forced":                       {def: "state_remove.yaml", state: "rpool/USERDATA/user3_3333", user: "user3", force: true},
		"Can’t remove user filesystem linked to system state with deps linked to system state":   {def: "state_remove.yaml", state: "rpool/USERDATA/user1_abcd", user: "user1", wantErr: true, wantConfirmationErr: true, isNoOp: true},
		"Remove user filesystem linked to system state with deps linked to system state, forced": {def: "state_remove.yaml", state: "rpool/USERDATA/user1_abcd", user: "user1", force: true},

		// Shared user state handling
		"Remove shared user state linked to a history state. Deps are not removed":                         {def: "state_remove.yaml", state: "rpool/USERDATA/root_bcde-rpool.ROOT.ubuntu-5678", user: "root", force: true},
		"Remove shared user state linked to current machine state. Deps are not removed":                   {def: "state_remove.yaml", state: "rpool/USERDATA/root_bcde-rpool.ROOT.ubuntu-1234", user: "root", force: true},
		"Remove shared user state as a dependency of other state. Deps are removed":                        {def: "m_shared_userstate_on_clones.yaml", state: "rpool/USERDATA/user_abcd", user: "user", force: true},
		"Remove shared user state on different matchines as a dependency of other state. Deps are removed": {def: "m_shared_userstate_on_two_machines.yaml", state: "rpool/USERDATA/user_abcd", user: "user", force: true},

		"No state given": {def: "m_with_userdata.yaml", wantErr: true, isNoOp: true},
		"Error on trying to remove current state":    {def: "m_with_userdata.yaml", currentStateID: "rpool/ROOT/ubuntu_1234", state: "rpool/ROOT/ubuntu_1234", wantErr: true, isNoOp: true},
		"Error on destroy state, one dataset":        {def: "m_with_userdata.yaml", state: "rpool/ROOT/ubuntu_1234", destroyErrDS: []string{}, wantErr: true, isNoOp: true},
		"Error on destroy user state, with datasets": {def: "state_remove.yaml", state: "rpool/USERDATA/user5_for-manual-clone", user: "user5", force: true, destroyErrDS: []string{}, wantErr: true},
	}

	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir, cleanup := testutils.TempDir(t)
			defer cleanup()

			libzfs := testutils.GetMockZFS(t)
			fPools := testutils.NewFakePools(t, filepath.Join("testdata", tc.def), testutils.WithLibZFS(libzfs))
			defer fPools.Create(dir)()

			ms, err := machines.New(context.Background(), generateCmdLine(tc.currentStateID), machines.WithLibZFS(libzfs))
			if err != nil {
				t.Error("expected success but got an error scanning for machines", err)
			}

			initMachines := ms.CopyForTests(t)
			lzfs := libzfs.(*mock.LibZFS)
			lzfs.ErrOnDestroyDS(tc.destroyErrDS)

			err = ms.RemoveState(context.Background(), tc.state, tc.user, tc.force, false)
			if err != nil {
				if !tc.wantErr {
					t.Fatalf("expected no error but got: %v", err)
				}
				var e *machines.ErrStateRemovalNeedsConfirmation

				if tc.wantConfirmationErr {
					assert.True(t, errors.As(err, &e), "expected ErrStateRemovalNeedsConfirmation error type")
				} else {
					assert.False(t, errors.As(err, &e), "don't expect ErrStateRemovalNeedsConfirmation error type")
				}
				return
			}
			if err == nil && tc.wantErr {
				t.Fatal("expected an error but got none")
			}

			if tc.isNoOp {
				assertMachinesEquals(t, initMachines, ms)
			} else {
				assertMachinesToGolden(t, ms)
				assertMachinesNotEquals(t, initMachines, ms)
			}

			machinesAfterRescan, err := machines.New(context.Background(), generateCmdLine(tc.currentStateID), machines.WithLibZFS(libzfs))
			if err != nil {
				t.Error("expected success but got an error scanning for machines", err)
			}
			assertMachinesEquals(t, machinesAfterRescan, ms)
		})
	}
}

func TestIDToState(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		name string
		user string

		wantState string
		wantErr   bool
	}{
		"Match full system path ID": {name: "rpool/ROOT/ubuntu_1234", wantState: "rpool/ROOT/ubuntu_1234"},
		"Match full user path ID":   {name: "rpool/USERDATA/user1_abcd", user: "user1", wantState: "rpool/USERDATA/user1_abcd"},

		"Match suffix system ID":       {name: "5678", wantState: "rpool/ROOT/ubuntu_5678"},
		"Match dataset path system ID": {name: "ubuntu_5678", wantState: "rpool/ROOT/ubuntu_5678"},
		"Match unique system snapshot": {name: "snap1", wantState: "rpool/ROOT/ubuntu_1234@snap1"},

		"Limit search on duplicated snapshot name to a single user": {name: "snap1", user: "user1", wantState: "rpool/USERDATA/user1_abcd@snap1"},

		// User datasets shared between machines
		"Match on user generated ID":                                                         {name: "jklm-rpool.ROOT.ubuntu-1234", user: "user2", wantState: "rpool/USERDATA/user2_jklm"},
		"Doesn’t match on user dataset regular name":                                         {name: "rpool/USERDATA/user2_jklm", user: "user2", wantErr: true},
		"User data attached to 2 machines but only linked to one system state isn’t renamed": {name: "rpool/USERDATA/user3_mnop", user: "user3", wantState: "rpool/USERDATA/user3_mnop"},

		// Multiple matches
		"Multiple states match system suffix ID": {name: "1234", wantErr: true},
		"Multiple states match user suffix ID":   {name: "abcd", user: "user1", wantErr: true},

		// No match
		"Empty name":         {name: "", wantErr: true},
		"No match at all":    {name: "/doesntexists", wantErr: true},
		"User doesn’t exist": {name: "foo", user: "userfoo", wantErr: true},
		"No match on full path ID search without user provided":    {name: "rpool/USERDATA/user1_abcd", wantErr: true},
		"No match on full path ID search with wrong user provided": {name: "rpool/USERDATA/user1_abcd", user: "userfoo", wantErr: true},
	}

	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			dir, cleanup := testutils.TempDir(t)
			defer cleanup()

			libzfs := testutils.GetMockZFS(t)
			fPools := testutils.NewFakePools(t, filepath.Join("testdata", "state_idtostate.yaml"), testutils.WithLibZFS(libzfs))
			defer fPools.Create(dir)()

			_, err := zfs.New(context.Background(), zfs.WithLibZFS(libzfs))
			if err != nil {
				t.Fatalf("couldn’t create original zfs datasets state")
			}

			ms, err := machines.New(context.Background(), "", machines.WithLibZFS(libzfs))
			if err != nil {
				t.Error("expected success but got an error scanning for machines", err)
			}

			got, err := ms.IDToState(context.Background(), tc.name, tc.user)

			if err != nil {
				if !tc.wantErr {
					t.Fatalf("Got an error when expecting none: %v", err)
				}
				return
			} else if tc.wantErr {
				t.Fatalf("Expected an error but got none")
			}

			assert.Equal(t, tc.wantState, got.ID, "didn't get expected state")
		})
	}
}

func TestGC(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		def        string
		all        bool
		configPath string

		destroyErrDS []string

		isNoOp  bool
		wantErr bool
	}{
		/***** System states only tests *****/
		"Follow bucket policy":                       {def: "gc_system_only.yaml"},
		"Follow bucket policy with one empty bucket": {def: "gc_system_only.yaml", configPath: "one_empty_bucket.conf"},
		"Existing buckets have enough capacity":      {def: "gc_system_only.yaml", configPath: "not_enough_snapshots.conf"},

		"No snapshot, keep everything":                         {def: "m_with_userdata.yaml", isNoOp: true},
		"Keep previous and current day, purge everything else": {def: "gc_system_only.yaml", configPath: "purge_all_zsys.conf"},
		"Keep current day, purge everything else":              {def: "gc_system_only.yaml", configPath: "purge_but_previous_zsys.conf"},
		"Non zsys systems are ignored":                         {def: "gc_system_only_non_zsys.yaml", isNoOp: true},
		"Keep more snapshots than simply last day has":         {def: "gc_system_only.yaml", configPath: "keep_many_snapshots.conf"},

		"Manual snapshot which should be deleted is kept":     {def: "gc_system_only_with_manual_snapshot.yaml"},
		"Manual snapshot which should be deleted isnt't kept": {def: "gc_system_only_with_manual_snapshot.yaml", all: true},

		"Clone and dependencies are collected within the same bucket":                  {def: "gc_system_only_with_clone_same_bucket.yaml"},
		"Manual clone without last used is collected":                                  {def: "gc_system_only_with_manual_clone.yaml"},
		"Clone having snapshots and dependencies are collected within the same bucket": {def: "gc_system_only_with_clone_same_bucket_with_dep.yaml"},
		"Keep clone and dependencies having manual snapshots":                          {def: "gc_system_only_with_clone_same_bucket_with_manual_dep.yaml"},
		"Clone cloned and all dependencies are collected within the same bucket":       {def: "gc_system_only_with_clone_same_bucket_with_clone.yaml"},
		"Clone and dependencies are collected trans bucket (clone after snapshot)":     {def: "gc_system_only_with_clone_different_buckets.yaml"},
		"Clone with dependencies in bucket before, same and after":                     {def: "gc_system_only_with_clone_and_snapshots_different_buckets.yaml"},

		"Subdataset with some snapshots shared with it":              {def: "gc_system_only_with_children.yaml"},
		"Subdataset with some snapshots only it are kept":            {def: "gc_system_only_with_children_snapshots_only_on_subdataset.yaml"},
		"Subdataset with snapshot keeps clone":                       {def: "gc_system_only_with_clone_same_bucket_with_snapshot_on_subdataset.yaml"},
		"Subdataset and clone are collected":                         {def: "gc_system_only_with_clone_same_bucket_with_snapshot_on_subdataset_and_main_state.yaml"},
		"Subdataset with manual clone on subdataset isn’t collected": {def: "gc_system_only_with_clone_same_bucket_with_snapshot_on_subdataset_and_main_state_with_manual_clone_on_subdataset.yaml"},

		/***** User states tests *****/
		"Follow bucket policy with users":                      {def: "gc_system_with_users.yaml"},
		"Follow bucket policy with users and one empty bucket": {def: "gc_system_with_users_one_empty_bucket.yaml", configPath: "one_empty_bucket.conf", isNoOp: true},
		"Keep more user snapshots than simply last day has":    {def: "gc_system_with_users.yaml", configPath: "keep_many_snapshots.conf"},

		// User clones
		"Remove user clone state":                                                                     {def: "gc_system_with_users_clone.yaml"},
		"Remove user clone state with subdataset":                                                     {def: "gc_system_with_users_clone_subdataset.yaml"},
		"Don't remove user clone state with snapshot on it kept":                                      {def: "gc_system_with_users_clone_with_manual_snapshot.yaml", isNoOp: true},
		"Don't remove user clone state with snapshot on subdataset":                                   {def: "gc_system_with_users_clone_subdataset_with_manual_snapshot.yaml", isNoOp: true},
		"Don't remove user clone state linked to a system state":                                      {def: "gc_system_with_users_clone_linked_to_system_state.yaml", isNoOp: true},
		"Ensure user clones are accounted by policy":                                                  {def: "gc_system_with_users_with_untagged_clones.yaml"},
		"Remove unassociated user clone after deleting its snapshot":                                  {def: "gc_system_with_users_clone_with_auto_snapshot.yaml"},
		"Remove unassociated user clone after deleting its snapshot which was linked to system state": {def: "gc_system_with_users_clone_with_auto_snapshot_attached_to_system_state.yaml"},

		// User clone attached to multiple states/machines
		"Users and clones on shared system state":                     {def: "gc_system_with_users_and_clones_shared_system_state.yaml"},
		"Users and clones on different machines history":              {def: "gc_system_with_users_and_clones_different_machines_history_only.yaml"},
		"Users and clones on different machines, one is active state": {def: "gc_system_with_users_and_clones_different_machines.yaml"},

		// Unlinked user test cases
		"Remove simple unlinked user dataset":                                             {def: "gc_system_with_unlinked_users.yaml"},
		"Remove unlinked user dataset and any snapshot":                                   {def: "gc_system_with_unlinked_users_and_snapshot.yaml"},
		"Remove unlinked user dataset and any unmanaged user clone":                       {def: "gc_system_with_unlinked_users_unmanaged_clone.yaml"},
		"Can't remove unlinked user dataset if it has a manual clone in USERDATA":         {def: "gc_system_with_unlinked_users_unmanaged_clone_nobootfs_dataset.yaml", isNoOp: true},
		"Can't remove unlinked user dataset if it has a manual clone outside of USERDATA": {def: "gc_system_with_unlinked_users_unmanaged_user_clone.yaml", isNoOp: true},
		"Remove unlinked clone of a manual filesystem dataset":                            {def: "gc_system_with_unlinked_users_unmanaged_clone_bootfs_on_clone.yaml"},

		// Failed revert
		"Failed revert - no lastused on system":        {def: "gc_system_failed_revert.yaml"},
		"Failed boot - no lastused on user and system": {def: "gc_system_with_users_failed_boot.yaml"},

		// Deletion prevention (no infinite loop)
		"Manual user snapshot which should be deleted is kept": {def: "gc_system_with_users_manual_snapshots.yaml", isNoOp: true},
		"Users and clones with undeletable snapshot":           {def: "gc_system_with_users_and_clones_undeletable_snapshot.yaml"},
		"Destroy failed on system dataset":                     {def: "gc_system_with_users_clone_with_auto_snapshot_attached_to_system_state.yaml", destroyErrDS: []string{"rpool/ROOT/ubuntu_1234@autozsys_20191230-1710"}, isNoOp: true},
		"Destroy failed on user dataset":                       {def: "gc_system_with_users_clone.yaml", destroyErrDS: []string{"rpool/USERDATA/user1_clone"}, isNoOp: true},
		"Destroy failed on unlinked user dataset":              {def: "gc_system_with_unlinked_users_unmanaged_clone_bootfs_on_clone.yaml", destroyErrDS: []string{"rpool/USERDATA/user2_clone"}, isNoOp: true},

		// Error cases
		"Error fails to destroy state are kept": {def: "gc_system_with_users.yaml", destroyErrDS: []string{}, isNoOp: true},
	}

	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir, cleanup := testutils.TempDir(t)
			defer cleanup()

			libzfs := testutils.GetMockZFS(t)
			fPools := testutils.NewFakePools(t, filepath.Join("testdata", tc.def), testutils.WithLibZFS(libzfs))
			defer fPools.Create(dir)()

			if tc.configPath == "" {
				tc.configPath = "default.conf"
			}
			tc.configPath = filepath.Join("testdata", "confs", tc.configPath)

			z, err := zfs.New(context.Background(), zfs.WithLibZFS(libzfs))
			if err != nil {
				t.Fatalf("couldn’t create original zfs datasets state")
			}
			for _, d := range z.Datasets() {
				if d.BootfsDatasets == "-" {
					tz, _ := z.NewTransaction(context.Background())
					defer tz.Done()
					if err := tz.SetProperty(libzfsadapter.BootfsDatasetsProp, "", d.Name, false); err != nil {
						t.Fatalf("couldn’t erase  BootfsDatasetsProp: %v", err)
					}
					tz.Done()
				}
			}
			ms, err := machines.New(context.Background(), "", machines.WithLibZFS(libzfs),
				machines.WithTime(testutils.FixedTime{}), machines.WithConfig(tc.configPath))
			if err != nil {
				t.Error("expected success but got an error scanning for machines", err)
			}

			initMachines := ms.CopyForTests(t)
			lzfs := libzfs.(*mock.LibZFS)
			lzfs.ErrOnDestroyDS(tc.destroyErrDS)

			err = ms.GC(context.Background(), tc.all)
			if err != nil {
				if !tc.wantErr {
					t.Fatalf("expected no error but got: %v", err)
				}
				return
			}
			if err == nil && tc.wantErr {
				t.Fatal("expected an error but got none")
			}

			if tc.isNoOp {
				assertMachinesEquals(t, initMachines, ms)
			} else {
				assertMachinesToGolden(t, ms)
				assertMachinesNotEquals(t, initMachines, ms)
			}

			machinesAfterRescan, err := machines.New(context.Background(), "", machines.WithLibZFS(libzfs))
			if err != nil {
				t.Error("expected success but got an error scanning for machines", err)
			}
			assertMachinesEquals(t, machinesAfterRescan, ms)
		})
	}
}

func BenchmarkNewDesktop(b *testing.B) {
	config.SetVerboseMode(0)
	defer func() { config.SetVerboseMode(1) }()

	dir, cleanup := testutils.TempDir(b)
	defer cleanup()

	libzfs := testutils.GetMockZFS(b)
	fPools := testutils.NewFakePools(b, filepath.Join("testdata", "m_layout1_machines_with_snapshots_clones.yaml"), testutils.WithLibZFS(libzfs))
	defer fPools.Create(dir)()

	for n := 0; n < b.N; n++ {
		machines.New(context.Background(), generateCmdLine("rpool/ROOT/ubuntu_5678"), machines.WithLibZFS(libzfs))
	}
}

func BenchmarkNewServer(b *testing.B) {
	config.SetVerboseMode(0)
	defer func() { config.SetVerboseMode(1) }()

	dir, cleanup := testutils.TempDir(b)
	defer cleanup()

	libzfs := testutils.GetMockZFS(b)
	fPools := testutils.NewFakePools(b, filepath.Join("testdata", "m_layout2_machines_with_snapshots_clones.yaml"), testutils.WithLibZFS(libzfs))
	defer fPools.Create(dir)()

	for n := 0; n < b.N; n++ {
		machines.New(context.Background(), generateCmdLine("rpool/ROOT/ubuntu_5678"), machines.WithLibZFS(libzfs))
	}
}

// assertMachinesToGolden compares got slice of machines to reference files, based on test name.
func assertMachinesToGolden(t *testing.T, got machines.Machines) {
	t.Helper()

	want := machines.Machines{}
	got.MakeComparable()
	testutils.LoadFromGoldenFile(t, got, &want)

	assertMachinesEquals(t, want, got)
}

// assertMachinesEquals compares two machines
func assertMachinesEquals(t *testing.T, m1, m2 machines.Machines) {
	t.Helper()

	m1.MakeComparable()
	m2.MakeComparable()

	if diff := cmp.Diff(m1, m2, cmpopts.EquateEmpty(),
		cmp.AllowUnexported(machines.Machines{}),
		cmpopts.IgnoreUnexported(zfs.Dataset{}, zfs.DatasetProp{})); diff != "" {
		t.Errorf("Machines mismatch (-want +got):\n%s", diff)
	}
}

// assertMachinesNotEquals ensure that two machines are differents
func assertMachinesNotEquals(t *testing.T, m1, m2 machines.Machines) {
	t.Helper()

	m1.MakeComparable()
	m2.MakeComparable()

	if diff := cmp.Diff(m1, m2, cmpopts.EquateEmpty(),
		cmp.AllowUnexported(machines.Machines{}),
		cmpopts.IgnoreUnexported(zfs.Dataset{}, zfs.DatasetProp{})); diff == "" {
		t.Errorf("Machines are equals where we expected not to:\n%+v", pp.Sprint(m1))
	}
}

// generateCmdLine returns a command line with fake boot arguments
func generateCmdLine(datasetAndBoot string) string {
	return "aaaaa bbbbb root=ZFS=" + datasetAndBoot + " ccccc"
}

// generateCmdLineWithRevert returns a command line with fake boot and a revert user data argument
func generateCmdLineWithRevert(datasetAndBoot string) string {
	return generateCmdLine(datasetAndBoot) + " " + machines.RevertUserDataTag
}

// getDefaultValue returns default value for this parameter
func getDefaultValue(v, defaultVal string) string {
	if v == "" {
		return defaultVal
	}
	if v == "[empty]" {
		return ""
	}

	return v
}
