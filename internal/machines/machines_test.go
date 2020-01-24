package machines_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/k0kubun/pp"
	"github.com/stretchr/testify/assert"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/machines"
	"github.com/ubuntu/zsys/internal/testutils"
	"github.com/ubuntu/zsys/internal/zfs"
)

func init() {
	config.SetVerboseMode(1)
}

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

		// Userdata user snapshots
		"Userdata has a user snapshot":              {def: "m_with_userdata_user_snapshot.yaml"},
		"Userdata with underscore in snapshot name": {def: "m_with_userdata_snapshotname_with_underscore.yaml"},

		// Persistent special cases
		"One machine, with persistent disabled":  {def: "m_with_persistent_canmount_noauto.yaml"},
		"Two machines have the same persistents": {def: "m_two_machines_with_persistent.yaml"},
		"Snapshot has the same persistents":      {def: "m_snapshot_with_persistent.yaml"},
		"Clone has the same persistents":         {def: "m_clone_with_persistent.yaml"},

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
	}

	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir, cleanup := testutils.TempDir(t)
			defer cleanup()

			libzfs := getLibZFS(t)
			fPools := testutils.NewFakePools(t, filepath.Join("testdata", tc.def), testutils.WithLibZFS(libzfs))
			defer fPools.Create(dir)()

			if tc.mountedDataset != "" {
				lzfs := libzfs.(*zfs.LibZFSMock)
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

	libzfs := getLibZFS(t)
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
			libzfs := getLibZFS(t)
			fPools := testutils.NewFakePools(t, filepath.Join("testdata", tc.def), testutils.WithLibZFS(libzfs))
			defer fPools.Create(dir)()

			lzfs := libzfs.(*zfs.LibZFSMock)
			if tc.mountedDataset != "" {
				lzfs.SetDatasetAsMounted(tc.mountedDataset, true)
			}

			initMachines, err := machines.New(context.Background(), tc.cmdline, machines.WithLibZFS(libzfs))
			if err != nil {
				t.Error("expected success but got an error scanning for machines", err)
			}
			ms := initMachines

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

	libzfs := getLibZFS(t)
	fPools := testutils.NewFakePools(t, filepath.Join("testdata", "m_layout2_machines_with_snapshots_clones_reverting.yaml"), testutils.WithLibZFS(libzfs))
	defer fPools.Create(dir)()

	ms1, err := machines.New(context.Background(), generateCmdLineWithRevert("rpool/ROOT/ubuntu_5678"), machines.WithLibZFS(libzfs))
	if err != nil {
		t.Error("expected success but got an error at first scan on machines", err)
	}

	hasChanged, err := ms1.EnsureBoot(context.Background())
	if err != nil {
		t.Fatalf("expected no error but got: %v", err)
	}

	assert.True(t, hasChanged, "expected first boot to signal a change, but got false")
	ms2 := ms1

	hasChanged, err = ms2.EnsureBoot(context.Background())
	if err != nil {
		t.Fatalf("expected no error but got: %v", err)
	}
	assert.False(t, hasChanged, "expected second boot to signal no change, but got true")

	assertMachinesEquals(t, ms1, ms2)
}

// TODO: not really idempotent, but should untag datasets that are tagged with destination datasets, maybe even destroy if it's the only one?
// check what happens in case of daemon-reload… Maybe should be a no-op if mounted (and we should simulate here mounting user datasets)
// Destroy if no LastUsed for user datasets?
// Destroy system non boot dataset?
func TestIdempotentBootSnapshotSuccess(t *testing.T) {
	t.Parallel()
	dir, cleanup := testutils.TempDir(t)
	defer cleanup()

	libzfs := getLibZFS(t)
	fPools := testutils.NewFakePools(t, filepath.Join("testdata", "m_layout2_machines_with_snapshots_clones_reverting.yaml"), testutils.WithLibZFS(libzfs))
	defer fPools.Create(dir)()

	lzfs := libzfs.(*zfs.LibZFSMock)
	lzfs.SetDatasetAsMounted("rpool/ROOT/ubuntu_4242", true)

	ms1, err := machines.New(context.Background(), generateCmdLineWithRevert("rpool/ROOT/ubuntu_5678@snap3"), machines.WithLibZFS(libzfs))
	if err != nil {
		t.Error("expected success but got an error at first scan on machines", err)
	}

	hasChanged, err := ms1.EnsureBoot(context.Background())
	if err != nil {
		t.Fatalf("expected no error but got: %v", err)
	}
	assert.True(t, hasChanged, "expected first boot to signal a change, but got false")

	hasChanged, err = ms1.Commit(context.Background())
	if err != nil {
		t.Fatal("Commit failed:", err)
	}
	assert.True(t, hasChanged, "expected first commit to signal a change, but got false")
	ms2 := ms1

	hasChanged, err = ms2.EnsureBoot(context.Background())
	if err != nil {
		t.Fatalf("expected no error but got: %v", err)
	}
	assert.False(t, hasChanged, "expected second boot to signal no change, but got true")

	assertMachinesEquals(t, ms1, ms2)
}

func TestIdempotentBootSnapshotBeforeCommit(t *testing.T) {
	t.Parallel()
	dir, cleanup := testutils.TempDir(t)
	defer cleanup()

	libzfs := getLibZFS(t)
	fPools := testutils.NewFakePools(t, filepath.Join("testdata", "m_layout2_machines_with_snapshots_clones_reverting.yaml"), testutils.WithLibZFS(libzfs))
	defer fPools.Create(dir)()

	lzfs := libzfs.(*zfs.LibZFSMock)
	lzfs.SetDatasetAsMounted("rpool/ROOT/ubuntu_4242", true)

	ms1, err := machines.New(context.Background(), generateCmdLineWithRevert("rpool/ROOT/ubuntu_5678@snap3"), machines.WithLibZFS(libzfs))
	if err != nil {
		t.Error("expected success but got an error at first scan on machines", err)
	}

	hasChanged, err := ms1.EnsureBoot(context.Background())
	if err != nil {
		t.Fatalf("expected no error but got: %v", err)
	}
	assert.True(t, hasChanged, "expected first boot to signal a change, but got false")

	ms2 := ms1

	hasChanged, err = ms2.EnsureBoot(context.Background())
	if err != nil {
		t.Fatalf("expected no error but got: %v", err)
	}
	assert.False(t, hasChanged, "expected second boot to signal no change, but got true")

	assertMachinesEquals(t, ms1, ms2)
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
			libzfs := getLibZFS(t)
			fPools := testutils.NewFakePools(t, filepath.Join("testdata", tc.def), testutils.WithLibZFS(libzfs))
			defer fPools.Create(dir)()

			initMachines, err := machines.New(context.Background(), tc.cmdline, machines.WithLibZFS(libzfs))
			if err != nil {
				t.Error("expected success but got an error scanning for machines", err)
			}
			lzfs := libzfs.(*zfs.LibZFSMock)

			lzfs.ErrOnScan(tc.scanErr)
			lzfs.ErrOnSetProperty(tc.setPropertyErr)
			lzfs.ErrOnPromote(tc.promoteErr)
			lzfs.ForceLastUsedTime(true)
			ms := initMachines

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

	libzfs := getLibZFS(t)
	fPools := testutils.NewFakePools(t, filepath.Join("testdata", "m_layout2_machines_with_snapshots_clones_no_user_revert.yaml"), testutils.WithLibZFS(libzfs))
	defer fPools.Create(dir)()

	lzfs := libzfs.(*zfs.LibZFSMock)
	lzfs.SetDatasetAsMounted("rpool/ROOT/ubuntu_9876", true)
	lzfs.ForceLastUsedTime(true)

	ms1, err := machines.New(context.Background(), generateCmdLine("rpool/ROOT/ubuntu_9876"), machines.WithLibZFS(libzfs))
	if err != nil {
		t.Error("expected success but got an error at first scan on machines", err)
	}

	hasChanged, err := ms1.Commit(context.Background())
	if err != nil {
		t.Fatal("first commit failed:", err)
	}
	assert.True(t, hasChanged, "expected first commit to signal a change, but got false")

	ms2 := ms1

	hasChanged, err = ms2.Commit(context.Background())
	if err != nil {
		t.Fatal("second commit failed:", err)
	}
	assert.False(t, hasChanged, "expected second commit to signal no change, but got true")

	assertMachinesEquals(t, ms1, ms2)
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
			libzfs := getLibZFS(t)
			fPools := testutils.NewFakePools(t, filepath.Join("testdata", tc.def), testutils.WithLibZFS(libzfs))
			defer fPools.Create(dir)()

			lzfs := libzfs.(*zfs.LibZFSMock)
			lzfs.ForceLastUsedTime(true)

			initMachines, err := machines.New(context.Background(), tc.cmdline, machines.WithLibZFS(libzfs))
			if err != nil {
				t.Error("expected success but got an error scanning for machines", err)
			}

			ms := initMachines

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
			libzfs := getLibZFS(t)
			fPools := testutils.NewFakePools(t, filepath.Join("testdata", tc.def), testutils.WithLibZFS(libzfs))
			defer fPools.Create(dir)()

			lzfs := libzfs.(*zfs.LibZFSMock)
			lzfs.ForceLastUsedTime(true)

			initMachines, err := machines.New(context.Background(), generateCmdLine("rpool/ROOT/ubuntu_1234"), machines.WithLibZFS(libzfs))
			if err != nil {
				t.Error("expected success but got an error scanning for machines", err)
			}

			ms := initMachines

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
			libzfs := getLibZFS(t)
			fPools := testutils.NewFakePools(t, filepath.Join("testdata", tc.def), testutils.WithLibZFS(libzfs))
			defer fPools.Create(dir)()

			if tc.cmdline == "" {
				tc.cmdline = generateCmdLine("rpool/ROOT/ubuntu_1234")
			}

			initMachines, err := machines.New(context.Background(), tc.cmdline, machines.WithLibZFS(libzfs))
			if err != nil {
				t.Error("expected success but got an error scanning for machines", err)
			}
			lzfs := libzfs.(*zfs.LibZFSMock)

			lzfs.ForceLastUsedTime(true)
			ms := initMachines

			err = ms.CreateSystemSnapshot(context.Background(), tc.snapshotName)
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
		"Take one snapshot":       {def: "m_with_userdata.yaml"},
		"Give a name to snapshot": {def: "m_with_userdata.yaml", snapshotName: "my_snapshot"},

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
			libzfs := getLibZFS(t)
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

			initMachines, err := machines.New(context.Background(), tc.cmdline, machines.WithLibZFS(libzfs))
			if err != nil {
				t.Error("expected success but got an error scanning for machines", err)
			}
			lzfs := libzfs.(*zfs.LibZFSMock)

			lzfs.ForceLastUsedTime(true)
			ms := initMachines

			err = ms.CreateUserSnapshot(context.Background(), tc.userName, tc.snapshotName)
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
			machinesAfterRescan, err := machines.New(context.Background(), tc.cmdline, machines.WithLibZFS(libzfs))
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

	libzfs := getLibZFS(b)
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

	libzfs := getLibZFS(b)
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

type testhelper interface {
	Helper()
}

// TODO: for now, we can only run with mock zfs system
func getLibZFS(t testhelper) testutils.LibZFSInterface {
	t.Helper()

	fmt.Println("Running tests with mocked libzfs")
	mock := zfs.NewLibZFSMock()
	return &mock
}
