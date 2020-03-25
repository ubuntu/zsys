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
		"Two machines maps with different user datasets":                 {def: "m_two_machines_with_different_userdata.yaml"},
		"Two machines maps with same user datasets":                      {def: "m_two_machines_with_same_userdata.yaml"},
		"User dataset attached to nothing":                               {def: "m_with_unlinked_userdata.yaml"}, // Userdata are still listed in the "all" list for switch to noauto.
		"User dataset attached to nothing but ignored with canmount off": {def: "m_with_unlinked_userdata_canmount_off.yaml"},
		"Snapshot with user dataset":                                     {def: "m_snapshot_with_userdata.yaml"},
		"Clone with user dataset":                                        {def: "m_clone_with_userdata.yaml"},
		"Snapshot with user dataset with children":                       {def: "m_snapshot_with_userdata_with_children.yaml"},
		"Clone with user dataset with children":                          {def: "m_clone_with_userdata_with_children.yaml"},
		"Clone with user dataset with children manually created":         {def: "m_clone_with_userdata_with_children_manually_created.yaml"},
		"Userdata with children associated only to one state":            {def: "m_with_userdata_child_associated_one_state.yaml"},
		"Userdata is linked to no machines":                              {def: "m_with_userdata_linked_to_no_machines.yaml"},
		// FIXME: see comment in machines.go. we should have user1_clone attached to the machine
		// consider other cases with it having snapshots, being attached as well and clone on this clone
		"User clone without bootfs dataset is still attached to the right machine via its snapshot": {def: "gc_system_with_users_clone.yaml"},

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

		wantErr bool
		isNoOp  bool
	}{
		"Take one snapshot":       {def: "m_with_userdata.yaml"},
		"Give a name to snapshot": {def: "m_with_userdata.yaml", snapshotName: "my_snapshot"},

		"Children on system datasets": {def: "m_with_userdata_children_on_system.yaml"},
		"Children on user datasets":   {def: "m_with_userdata_children_on_user.yaml"},
		"Children on user datasets with one child non associated with current machine": {def: "m_with_userdata_child_associated_one_state.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_9999")},

		"No associated userdata": {def: "d_one_machine_with_children.yaml", cmdline: generateCmdLine("rpool")},

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

		wantErr bool
		isNoOp  bool
	}{
		"Take one snapshot":                                   {def: "m_with_userdata.yaml"},
		"Give a name to snapshot":                             {def: "m_with_userdata.yaml", snapshotName: "my_snapshot"},
		"Take one snapshot on a machine with other snapshots": {def: "m_snapshot_with_userdata.yaml"},

		"Children on user datasets": {def: "m_with_userdata_children_on_user.yaml"},
		"Children on user datasets with one child non associated with current machine": {def: "m_with_userdata_child_associated_one_state.yaml", cmdline: generateCmdLine("rpool/ROOT/ubuntu_9999")},

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

		destroyErr bool

		isNoOp     bool
		wantErr    bool
		wantDepErr bool
	}{
		"Remove system state, one dataset": {def: "m_with_userdata.yaml", state: "rpool/ROOT/ubuntu_1234"},

		// FIXME: miss bpool and bpool/BOOT from golden file
		"Remove system state, complex with boot, children and user datasets": {def: "m_layout1_one_machine.yaml", state: "rpool/ROOT/ubuntu_1234"},
		"Remove one system snapshot only":                                    {def: "state_remove_internal.yaml", state: "rpool/ROOT/ubuntu_1234@snap3"},
		"Removing system state try to delete its snapshots":                  {def: "state_remove.yaml", state: "rpool/ROOT/ubuntu_6789", wantErr: true, wantDepErr: true},
		"Removing system state deletes its snapshots":                        {def: "state_remove.yaml", state: "rpool/ROOT/ubuntu_6789", force: true},

		"Remove system state, with system and users snapshots and clones":         {def: "m_layout1_machines_with_snapshots_clones.yaml", state: "rpool/ROOT/ubuntu_1234", wantErr: true, wantDepErr: true, isNoOp: true},
		"Remove system state, with system and users snapshots and clones, forced": {def: "m_layout1_machines_with_snapshots_clones.yaml", state: "rpool/ROOT/ubuntu_1234", force: true},
		"Remove system state when user datasets are linked to 2 states":           {def: "state_remove_internal.yaml", state: "rpool/ROOT/ubuntu_5678"},
		"Remove system state, with datasets":                                      {def: "state_remove.yaml", state: "rpool/ROOT/ubuntu_1234", wantDepErr: true, wantErr: true, isNoOp: true},
		"Remove system state, with datasets, forced":                              {def: "state_remove.yaml", state: "rpool/ROOT/ubuntu_1234", force: true},

		"Remove user state, one dataset":                       {def: "state_remove.yaml", state: "rpool/USERDATA/user4_clone", user: "user4"},
		"Remove user state, one dataset, no user":              {def: "state_remove.yaml", state: "rpool/USERDATA/user4_clone", wantErr: true, isNoOp: true},
		"Remove user state, one dataset, wrong user":           {def: "state_remove.yaml", state: "rpool/USERDATA/user4_clone", user: "root", wantErr: true, isNoOp: true},
		"Remove user state, with snapshots and clones":         {def: "state_remove.yaml", user: "user6", state: "rpool/USERDATA/user6_clone1", wantErr: true, wantDepErr: true, isNoOp: true},
		"Remove user state, with snapshots and clones, forced": {def: "state_remove.yaml", user: "user6", state: "rpool/USERDATA/user6_clone1", force: true},
		"Remove user state, with datasets":                     {def: "state_remove.yaml", state: "rpool/USERDATA/user5_for-manual-clone@snapuser5", user: "user5", wantErr: true, wantDepErr: true, isNoOp: true},
		"Remove user state, with datasets, forced":             {def: "state_remove.yaml", state: "rpool/USERDATA/user5_for-manual-clone@snapuser5", user: "user5", force: true},
		"Remove user snapshot state":                           {def: "state_remove.yaml", state: "rpool/USERDATA/user1_efgh@snapuser2", user: "user1"},

		"No state given": {def: "m_with_userdata.yaml", wantErr: true, isNoOp: true},
		"Error on trying to remove current state":    {def: "m_with_userdata.yaml", currentStateID: "rpool/ROOT/ubuntu_1234", state: "rpool/ROOT/ubuntu_1234", wantErr: true, isNoOp: true},
		"Error on destroy state, one dataset":        {def: "m_with_userdata.yaml", state: "rpool/ROOT/ubuntu_1234", destroyErr: true, wantErr: true, isNoOp: true},
		"Error on destroy user state, with datasets": {def: "state_remove.yaml", state: "rpool/USERDATA/user5_for-manual-clone", user: "user5", force: true, destroyErr: true, wantErr: true},

		// TODO: check if we want to relax those
		"Can’t remove user leaf snapshot linked to system state":                 {def: "state_remove.yaml", state: "rpool/USERDATA/user1_abcd@snap1", user: "user1", wantErr: true, isNoOp: true},
		"Can’t remove user leaf snapshot linked to system state, even forced":    {def: "state_remove.yaml", state: "rpool/USERDATA/user1_abcd@snap1", user: "user1", force: true, wantErr: true, isNoOp: true},
		"Can’t remove user filesystem state linked to system state":              {def: "state_remove.yaml", state: "rpool/USERDATA/user1_abcd", user: "user1", wantErr: true, wantDepErr: true, isNoOp: true},
		"Can’t remove user filesystem state linked to system state, even forced": {def: "state_remove.yaml", state: "rpool/USERDATA/user1_abcd", user: "user1", force: true, wantErr: true, isNoOp: true},
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
			lzfs.ErrOnDestroy(tc.destroyErr)

			err = ms.RemoveState(context.Background(), tc.state, tc.user, tc.force, false)
			if err != nil {
				if !tc.wantErr {
					t.Fatalf("expected no error but got: %v", err)
				}
				var e *machines.ErrStateHasDependencies

				if tc.wantDepErr {
					assert.True(t, errors.As(err, &e), "expected ErrStateHasDependencies error type")
				} else {
					assert.False(t, errors.As(err, &e), "don't expect ErrStateHasDependencies error type")
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
		"Match on user generated ID":                 {name: "jklm-rpool.ROOT.ubuntu-1234", user: "user2", wantState: "rpool/USERDATA/user2_jklm"},
		"Doesn’t match on user dataset regular name": {name: "rpool/USERDATA/user2_jklm", user: "user2", wantErr: true},

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
