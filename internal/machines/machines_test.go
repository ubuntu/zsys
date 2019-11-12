package machines_test

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/k0kubun/pp"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/machines"
	"github.com/ubuntu/zsys/internal/testutils"
	"github.com/ubuntu/zsys/internal/zfs"
)

func init() {
	testutils.InstallUpdateFlag()
	config.SetVerboseMode(1)
}

func TestNew(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		def            string
		cmdline        string
		mountedDataset string
	}{
		"One machine, one dataset":            {def: "d_one_machine_one_dataset.json"},
		"One disabled machine":                {def: "d_one_disabled_machine.json"},
		"One machine with children":           {def: "d_one_machine_with_children.json"},
		"One machine with unordered children": {def: "d_one_machine_with_children_unordered.json"},

		"One machine, attach user datasets to machine": {def: "m_with_userdata.json"},
		"One machine, attach boot to machine":          {def: "m_with_separate_boot.json"},
		"One machine, with persistent datasets":        {def: "m_with_persistent.json"},

		// Machine <-> snapshot interactions
		"One machine with one snapshot":                              {def: "d_one_machine_with_one_snapshot.json"},
		"One machine with snapshot having less datasets than parent": {def: "d_one_machine_with_snapshot_less_datasets.json"},
		"One machine with snapshot having more datasets than parent": {def: "d_one_machine_with_snapshot_more_datasets.json"}, // This is actually one dataset being canmount=off

		// Machine <-> clones interactions
		"One machine with one clone":                                              {def: "d_one_machine_with_clone_dataset.json"},
		"One machine with one clone named before":                                 {def: "d_one_machine_with_clone_named_before.json"},
		"One machine with clones and snapshot on user datasets":                   {def: "m_with_clones_snapshots_userdata.json"},
		"One machine with a missing clone results in ignored machine (ZFS error)": {def: "d_one_machine_missing_clone.json"},

		// Zsys system special cases
		"One machine, one dataset, non zsys":   {def: "d_one_machine_one_dataset_non_zsys.json"},
		"Two machines, one zsys, one non zsys": {def: "d_two_machines_one_zsys_one_non_zsys.json"},

		// Last used special cases
		"One machine, no last used":                              {def: "d_one_machine_one_dataset_no_lastused.json"},
		"One machine with children, last used from root is used": {def: "d_one_machine_with_children_all_with_lastused.json"},

		// Boot special cases
		// TODO: separate boot and internal boot dataset? See grub
		"Two machines maps with different boot datasets":                 {def: "m_two_machines_with_separate_boot.json"},
		"Boot dataset attached to nothing":                               {def: "m_with_unlinked_boot.json"}, // boots are still listed in the "all" list for switch to noauto.
		"Boot dataset attached to nothing but ignored with canmount off": {def: "m_with_unlinked_boot_canmount_off.json"},
		"Snapshot with boot dataset":                                     {def: "m_snapshot_with_separate_boot.json"},
		"Clone with boot dataset":                                        {def: "m_clone_with_separate_boot.json"},
		"Snapshot with boot dataset with children":                       {def: "m_snapshot_with_separate_boot_with_children.json"},
		"Clone with boot dataset with children":                          {def: "m_clone_with_separate_boot_with_children.json"},
		"Clone with boot dataset with children manually created":         {def: "m_clone_with_separate_boot_with_children_manually_created.json"},

		// Userdata special cases
		"Two machines maps with different user datasets":                 {def: "m_two_machines_with_different_userdata.json"},
		"Two machines maps with same user datasets":                      {def: "m_two_machines_with_same_userdata.json"},
		"User dataset attached to nothing":                               {def: "m_with_unlinked_userdata.json"}, // Userdata are still listed in the "all" list for switch to noauto.
		"User dataset attached to nothing but ignored with canmount off": {def: "m_with_unlinked_userdata_canmount_off.json"},
		"Snapshot with user dataset":                                     {def: "m_snapshot_with_userdata.json"},
		"Clone with user dataset":                                        {def: "m_clone_with_userdata.json"},
		"Snapshot with user dataset with children":                       {def: "m_snapshot_with_userdata_with_children.json"},
		"Clone with user dataset with children":                          {def: "m_clone_with_userdata_with_children.json"},
		"Clone with user dataset with children manually created":         {def: "m_clone_with_userdata_with_children_manually_created.json"},

		// Persistent special cases
		"One machine, with persistent disabled":  {def: "m_with_persistent_canmount_noauto.json"},
		"Two machines have the same persistents": {def: "m_two_machines_with_persistent.json"},
		"Snapshot has the same persistents":      {def: "m_snapshot_with_persistent.json"},
		"Clone has the same persistents":         {def: "m_clone_with_persistent.json"},

		// Limit case with no machines
		"No machine": {def: "d_no_machine.json"},
		"No dataset": {def: "d_no_dataset.json"},

		// Real machine use cases
		"zsys layout desktop, one machine":                                 {def: "m_layout1_one_machine.json"},
		"zsys layout server, one machine":                                  {def: "m_layout2_one_machine.json"},
		"zsys layout desktop with snapshots and clones, multiple machines": {def: "m_layout1_machines_with_snapshots_clones.json"},
		"zsys layout desktop with cloning in progress, multiple machines":  {def: "m_layout1_machines_with_snapshots_clones_reverting.json"},
		"zsys layout server with snapshots and clones, multiple machines":  {def: "m_layout2_machines_with_snapshots_clones.json"},
		"zsys layout server with cloning in progress, multiple machines":   {def: "m_layout2_machines_with_snapshots_clones_reverting.json"},

		// cmdline selection
		"Select existing dataset machine":            {def: "d_one_machine_one_dataset.json", cmdline: generateCmdLine("rpool")},
		"Select correct machine":                     {def: "d_two_machines_one_dataset.json", cmdline: generateCmdLine("rpool2")},
		"Select main machine with snapshots/clones":  {def: "m_clone_with_persistent.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_1234")},
		"Select snapshot use mounted system dataset": {def: "m_clone_with_persistent.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_1234@snap1"), mountedDataset: "rpool/ROOT/ubuntu_1234"},
		"Select clone":                               {def: "m_clone_with_persistent.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Selected machine doesn't exist":             {def: "d_one_machine_one_dataset.json", cmdline: generateCmdLine("foo")},
		"Select existing dataset but not a machine":  {def: "m_with_persistent.json", cmdline: generateCmdLine("rpool/ROOT")},
	}

	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ds := machines.LoadDatasets(t, tc.def)
			for i := range ds {
				if ds[i].Name == tc.mountedDataset {
					ds[i].Mounted = true
				}
			}

			got := machines.New(context.Background(), ds, tc.cmdline)

			assertMachinesToGolden(t, got)
		})
	}
}

func TestIdempotentNew(t *testing.T) {
	t.Parallel()
	ds := machines.LoadDatasets(t, "m_layout2_machines_with_snapshots_clones.json")

	got1 := machines.New(context.Background(), ds, generateCmdLine("rpool/ROOT/ubuntu_5678"))
	got2 := machines.New(context.Background(), ds, generateCmdLine("rpool/ROOT/ubuntu_5678"))

	assertMachinesEquals(t, got1, got2)
}

func TestBoot(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		def                  string
		cmdline              string
		mountedDataset       string
		predictableSuffixFor string

		cloneErr       bool
		scanErr        bool
		setPropertyErr bool

		wantErr bool
		isNoOp  bool
	}{
		"One machine one dataset zsys":      {def: "d_one_machine_one_dataset.json", cmdline: generateCmdLine("rpool"), isNoOp: true},
		"One machine one dataset non zsys":  {def: "d_one_machine_one_dataset_non_zsys.json", cmdline: generateCmdLine("rpool"), isNoOp: true},
		"One machine one dataset, no match": {def: "d_one_machine_one_dataset.json", cmdline: generateCmdLine("rpoolfake"), isNoOp: true},

		// Two machines tests
		"Two machines, keep active":                        {def: "m_two_machines_simple.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_1234"), isNoOp: true},
		"Two machines, simple switch":                      {def: "m_two_machines_simple.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Two machines, with children":                      {def: "m_two_machines_recursive.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Two machines, both canmount on, simple switch":    {def: "m_two_machines_both_canmount_on.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Two machines, persistent":                         {def: "m_two_machines_with_persistent.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Two machines, separate user dataset":              {def: "m_two_machines_with_different_userdata.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Two machines, same user dataset":                  {def: "m_two_machines_with_same_userdata.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Two machines, separate boot":                      {def: "m_two_machines_with_separate_boot.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Reset boot on first main dataset, without suffix": {def: "m_main_dataset_without_suffix_and_clone.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu")},

		// Clone switch
		"Clone, keep main active":                                              {def: "m_clone_simple.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_1234"), isNoOp: true},
		"Clone, simple switch":                                                 {def: "m_clone_simple.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Clone, with children":                                                 {def: "m_clone_with_children.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Clone, both canmount on, simple switch":                               {def: "m_clone_both_canmount_on.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Clone, persistent":                                                    {def: "m_clone_with_persistent.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Clone, separate user dataset":                                         {def: "m_clone_with_userdata.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Clone, separate user dataset with children":                           {def: "m_clone_with_userdata_with_children.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Clone, separate user dataset with children manually created":          {def: "m_clone_with_userdata_with_children_manually_created.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Clone, separate reverted user dataset":                                {def: "m_clone_with_userdata.json", cmdline: generateCmdLineWithRevert("rpool/ROOT/ubuntu_5678")},
		"Clone, separate reverted user dataset with children":                  {def: "m_clone_with_userdata_with_children.json", cmdline: generateCmdLineWithRevert("rpool/ROOT/ubuntu_5678")},
		"Clone, separate reverted user dataset with children manually created": {def: "m_clone_with_userdata_with_children_manually_created.json", cmdline: generateCmdLineWithRevert("rpool/ROOT/ubuntu_5678")},
		"Clone, separate boot":                                                 {def: "m_clone_with_separate_boot.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Clone, separate boot with children":                                   {def: "m_clone_with_separate_boot_with_children.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Clone, separate boot with children manually created":                  {def: "m_clone_with_separate_boot_with_children_manually_created.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},

		// Reverting userdata
		"Reverting userdata, with children": {def: "m_clone_with_userdata_with_children_reverting.json",
			cmdline:              generateCmdLineWithRevert("rpool/ROOT/ubuntu_1234@snap1"),
			mountedDataset:       "rpool/ROOT/ubuntu_4242",
			predictableSuffixFor: "rpool/USERDATA/user1"},

		// Booting on snapshot on real machines
		"Desktop revert on snapshot": {def: "m_layout1_machines_with_snapshots_clones_reverting.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678@snap3"), mountedDataset: "rpool/ROOT/ubuntu_4242"},
		"Desktop revert on snapshot with userdata revert": {def: "m_layout1_machines_with_snapshots_clones_reverting.json",
			cmdline:              generateCmdLineWithRevert("rpool/ROOT/ubuntu_5678@snap3"),
			mountedDataset:       "rpool/ROOT/ubuntu_4242",
			predictableSuffixFor: "rpool/USERDATA/user1"},
		"Server revert on snapshot": {def: "m_layout2_machines_with_snapshots_clones_reverting.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678@snap3"), mountedDataset: "rpool/ROOT/ubuntu_4242"},
		"Server revert on snapshot with userdata revert": {def: "m_layout2_machines_with_snapshots_clones_reverting.json",
			cmdline:              generateCmdLineWithRevert("rpool/ROOT/ubuntu_5678@snap3"),
			mountedDataset:       "rpool/ROOT/ubuntu_4242",
			predictableSuffixFor: "rpool/USERDATA/user1"},

		// Error cases
		"No booted state found does nothing":       {def: "m_layout1_machines_with_snapshots_clones_reverting.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678@snap3"), isNoOp: true},
		"SetProperty fails":                        {def: "m_layout1_machines_with_snapshots_clones_reverting.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678@snap3"), mountedDataset: "rpool/ROOT/ubuntu_4242", setPropertyErr: true, wantErr: true},
		"SetProperty fails with revert":            {def: "m_layout1_machines_with_snapshots_clones_reverting.json", cmdline: generateCmdLineWithRevert("rpool/ROOT/ubuntu_5678@snap3"), mountedDataset: "rpool/ROOT/ubuntu_4242", setPropertyErr: true, wantErr: true},
		"Scan fails":                               {def: "m_layout1_machines_with_snapshots_clones_reverting.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678@snap3"), mountedDataset: "rpool/ROOT/ubuntu_4242", scanErr: true, wantErr: true},
		"Revert on created dataset without suffix": {def: "m_new_dataset_without_suffix_and_clone.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678@snap1"), mountedDataset: "rpool/ROOT/ubuntu", wantErr: true},
	}

	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ds := machines.LoadDatasets(t, tc.def)
			z := NewZfsMock(ds, tc.mountedDataset, tc.predictableSuffixFor, false, tc.cloneErr, tc.scanErr, tc.setPropertyErr, false)
			// Reload datasets from zfs, as mountedDataset can change .Mounted property (reused on consecutive Scan())
			var datasets []zfs.Dataset
			for _, d := range z.d {
				datasets = append(datasets, *d)
			}
			initMachines := machines.New(context.Background(), datasets, tc.cmdline)
			ms := initMachines

			hasChanged, err := ms.EnsureBoot(z)
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
				if hasChanged {
					t.Error("expected signalling no changed in commit but had some")
				}
				assertMachinesEquals(t, initMachines, ms)
			} else {
				if !hasChanged {
					t.Error("expected signalling changed in commit but told none")
				}
				assertMachinesToGolden(t, ms)
				assertMachinesNotEquals(t, initMachines, ms)
			}

			datasets, err = z.Scan()
			if err != nil {
				t.Fatal("couldn't rescan before checking final state:", err)
			}
			machinesAfterRescan := machines.New(context.Background(), datasets, tc.cmdline)
			assertMachinesEquals(t, machinesAfterRescan, ms)
		})
	}
}

func TestIdempotentBoot(t *testing.T) {
	t.Parallel()
	ds := machines.LoadDatasets(t, "m_layout2_machines_with_snapshots_clones_reverting.json")
	z := NewZfsMock(ds, "rpool/ROOT/ubuntu_4242", "rpool/USERDATA/user1", false, false, false, false, false)
	datasets, err := z.Scan()
	if err != nil {
		t.Fatal("couldn't scan for initial state:", err)
	}
	ms1 := machines.New(context.Background(), datasets, generateCmdLineWithRevert("rpool/ROOT/ubuntu_5678"))

	changed, err := ms1.EnsureBoot(z)
	if err != nil {
		t.Fatal("First EnsureBoot failed:", err)
	}
	if !changed {
		t.Fatal("expected first boot to signal a changed, but got false")
	}
	ms2 := ms1

	changed, err = ms2.EnsureBoot(z)
	if err != nil {
		t.Fatal("Second EnsureBoot failed:", err)
	}
	if changed {
		t.Fatal("expected second boot to signal no change, but got true")
	}

	assertMachinesEquals(t, ms1, ms2)
}

// TODO: not really idempotent, but should untag datasets that are tagged with destination datasets, maybe even destroy if it's the only one?
// check what happens in case of daemon-reload… Maybe should be a no-op if mounted (and we should simulate here mounting user datasets)
// Destroy if no LastUsed for user datasets?
// Destroy system non boot dataset?
func TestIdempotentBootSnapshotSuccess(t *testing.T) {
	t.Parallel()
	ds := machines.LoadDatasets(t, "m_layout2_machines_with_snapshots_clones_reverting.json")
	z := NewZfsMock(ds, "rpool/ROOT/ubuntu_4242", "rpool/USERDATA/user1", false, false, false, false, false)
	datasets, err := z.Scan()
	if err != nil {
		t.Fatal("couldn't scan for initial state:", err)
	}
	ms1 := machines.New(context.Background(), datasets, generateCmdLineWithRevert("rpool/ROOT/ubuntu_5678@snap3"))

	changed, err := ms1.EnsureBoot(z)
	if err != nil {
		t.Fatal("First EnsureBoot failed:", err)
	}
	if !changed {
		t.Fatal("expected first boot to signal a changed, but got false")
	}
	changed, err = ms1.Commit(z)
	if err != nil {
		t.Fatal("Commit failed:", err)
	}
	if !changed {
		t.Fatal("expected first commit to signal a changed, but got false")
	}
	ms2 := ms1

	changed, err = ms2.EnsureBoot(z)
	if err != nil {
		t.Fatal("Second EnsureBoot failed:", err)
	}
	if changed {
		t.Fatal("expected second commit to signal no change, but got true")
	}

	assertMachinesEquals(t, ms1, ms2)
}

func TestIdempotentBootSnapshotBeforeCommit(t *testing.T) {
	t.Parallel()
	ds := machines.LoadDatasets(t, "m_layout2_machines_with_snapshots_clones_reverting.json")
	z := NewZfsMock(ds, "rpool/ROOT/ubuntu_4242", "rpool/USERDATA/user1", false, false, false, false, false)
	datasets, err := z.Scan()
	if err != nil {
		t.Fatal("couldn't scan for initial state:", err)
	}
	ms1 := machines.New(context.Background(), datasets, generateCmdLineWithRevert("rpool/ROOT/ubuntu_5678@snap3"))

	changed, err := ms1.EnsureBoot(z)
	if err != nil {
		t.Fatal("First EnsureBoot failed:", err)
	}
	if !changed {
		t.Fatal("expected first boot to signal a changed, but got false")
	}
	ms2 := ms1

	changed, err = ms2.EnsureBoot(z)
	if err != nil {
		t.Fatal("Second EnsureBoot failed:", err)
	}
	if changed {
		t.Fatal("expected second boot to signal no change, but got true")
	}

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

		wantSignalNoChange bool
		wantErr            bool
		isNoOp             bool
	}{
		"One machine, commit one clone":                      {def: "d_one_machine_with_clone_to_promote.json", cmdline: generateCmdLine("rpool_clone")},
		"One machine, commit current":                        {def: "d_one_machine_with_clone_dataset.json", cmdline: generateCmdLine("rpool")},
		"One machine, update LastUsed but same kernel":       {def: "d_one_machine_with_clone_dataset.json", cmdline: generateCmdLine("rpool BOOT_IMAGE=vmlinuz-5.2.0-8-generic"), wantSignalNoChange: true},
		"One machine, update LastUsed and Kernel":            {def: "d_one_machine_with_clone_dataset.json", cmdline: generateCmdLine("rpool BOOT_IMAGE=vmlinuz-9.9.9-9-generic")},
		"One machine, set LastUsed and Kernel basename":      {def: "d_one_machine_with_clone_to_promote.json", cmdline: generateCmdLine("rpool BOOT_IMAGE=/boot/vmlinuz-9.9.9-9-generic")},
		"One machine, Kernel basename with already basename": {def: "d_one_machine_with_clone_to_promote.json", cmdline: generateCmdLine("rpool BOOT_IMAGE=vmlinuz-9.9.9-9-generic")},
		"One machine non zsys":                               {def: "d_one_machine_one_dataset_non_zsys.json", cmdline: generateCmdLine("rpool"), isNoOp: true, wantSignalNoChange: true},
		"One machine no match":                               {def: "d_one_machine_one_dataset.json", cmdline: generateCmdLine("rpoolfake"), isNoOp: true, wantSignalNoChange: true},

		"One machine with children":                                       {def: "m_clone_with_children_to_promote.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"One machine with children, LastUsed and kernel basename on root": {def: "m_clone_with_children_to_promote.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678 BOOT_IMAGE=/boot/vmlinuz-9.9.9-9-generic")},
		"Without suffix": {def: "m_main_dataset_without_suffix_and_clone_to_promote.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu")},

		"Separate user dataset, no user revert":                                {def: "m_clone_with_userdata_to_promote_no_user_revert.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Separate user dataset with children, no user revert":                  {def: "m_clone_with_userdata_with_children_to_promote_no_user_revert.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Separate user dataset with children manually created, no user revert": {def: "m_clone_with_userdata_with_children_manually_created_to_promote_no_user_revert.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Separate user dataset, user revert":                                   {def: "m_clone_with_userdata_to_promote_user_revert.json", cmdline: generateCmdLineWithRevert("rpool/ROOT/ubuntu_5678")},
		"Separate user dataset with children, user revert":                     {def: "m_clone_with_userdata_with_children_to_promote_user_revert.json", cmdline: generateCmdLineWithRevert("rpool/ROOT/ubuntu_5678")},
		"Separate user dataset with children manually created, user revert":    {def: "m_clone_with_userdata_with_children_manually_created_to_promote_user_revert.json", cmdline: generateCmdLineWithRevert("rpool/ROOT/ubuntu_5678")},

		"Separate boot":                                {def: "m_clone_with_separate_boot_to_promote.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Separate boot with children":                  {def: "m_clone_with_separate_boot_with_children_to_promote.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Separate boot with children manually created": {def: "m_clone_with_separate_boot_with_children_manually_created_to_promote.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},

		// Real machines
		"Desktop without user revert": {def: "m_layout1_machines_with_snapshots_clones_no_user_revert.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_9876")},
		"Desktop with user revert":    {def: "m_layout1_machines_with_snapshots_clones_user_revert.json", cmdline: generateCmdLineWithRevert("rpool/ROOT/ubuntu_9876")},
		"Server without user revert":  {def: "m_layout2_machines_with_snapshots_clones_no_user_revert.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_9876")},
		"Server with user revert":     {def: "m_layout2_machines_with_snapshots_clones_user_revert.json", cmdline: generateCmdLineWithRevert("rpool/ROOT/ubuntu_9876")},

		// Error cases
		"SetProperty fails (first)":  {def: "m_clone_with_userdata_with_children_to_promote_no_user_revert.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678"), setPropertyErr: true, wantErr: true},
		"SetProperty fails (second)": {def: "m_clone_with_userdata_to_promote_no_user_revert.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678"), setPropertyErr: true, wantErr: true},
		"Promote fails":              {def: "d_one_machine_with_clone_dataset.json", cmdline: generateCmdLine("rpool_clone"), promoteErr: true, wantErr: true},
		"Promote userdata fails":     {def: "m_clone_with_userdata_to_promote_user_revert.json", cmdline: generateCmdLineWithRevert("rpool/ROOT/ubuntu_5678"), promoteErr: true, wantErr: true},
		"Scan fails":                 {def: "d_one_machine_with_clone_dataset.json", cmdline: generateCmdLine("rpool"), scanErr: true, wantErr: true},
	}
	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ds := machines.LoadDatasets(t, tc.def)
			z := NewZfsMock(ds, "", "", false, false, tc.scanErr, tc.setPropertyErr, tc.promoteErr)
			// Do the Scan() manually, as we don't wan't to suffer from scanErr
			var datasets []zfs.Dataset
			for _, d := range z.d {
				datasets = append(datasets, *d)
			}
			initMachines := machines.New(context.Background(), datasets, tc.cmdline)
			ms := initMachines

			hasChanged, err := ms.Commit(z)
			if err != nil {
				if !tc.wantErr {
					t.Fatalf("expected no error but got: %v", err)
				}
				return
			}
			if err == nil && tc.wantErr {
				t.Fatal("expected an error but got none")
			}

			if tc.wantSignalNoChange != !hasChanged {
				t.Errorf("expected signalling change=%v, but got: %v", !tc.wantSignalNoChange, hasChanged)
			}
			if tc.isNoOp {
				assertMachinesEquals(t, initMachines, ms)
			} else {
				assertMachinesToGolden(t, ms)
				assertMachinesNotEquals(t, initMachines, ms)
			}

			datasets, err = z.Scan()
			if err != nil {
				t.Fatal("couldn't rescan before checking final state:", err)
			}
			machinesAfterRescan := machines.New(context.Background(), datasets, tc.cmdline)
			assertMachinesEquals(t, machinesAfterRescan, ms)
		})
	}
}

func TestIdempotentCommit(t *testing.T) {
	t.Parallel()
	ds := machines.LoadDatasets(t, "m_layout2_machines_with_snapshots_clones_no_user_revert.json")
	z := NewZfsMock(ds, "", "", false, false, false, false, false)
	datasets, err := z.Scan()
	if err != nil {
		t.Fatal("couldn't scan for initial state:", err)
	}
	ms1 := machines.New(context.Background(), datasets, generateCmdLine("rpool/ROOT/ubuntu_9876"))

	changed, err := ms1.Commit(z)
	if err != nil {
		t.Fatal("first commit failed:", err)
	}
	if !changed {
		t.Fatal("expected first commit to signal a changed, but got false")
	}
	ms2 := ms1

	changed, err = ms2.Commit(z)
	if err != nil {
		t.Fatal("second commit failed:", err)
	}
	if changed {
		t.Fatal("expected second commit to signal no change, but got true")
	}

	assertMachinesEquals(t, ms1, ms2)
}

func TestCreateUserData(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		def                  string
		user                 string
		homePath             string
		predictableSuffixFor string
		cmdline              string

		setPropertyErr bool
		createErr      bool
		scanErr        bool

		wantErr bool
		isNoOp  bool
	}{
		"One machine add user dataset":                  {def: "m_with_userdata.json"},
		"One machine add user dataset without userdata": {def: "m_without_userdata.json"},
		"One machine with no user, only userdata":       {def: "m_with_userdata_only.json"},
		"No attached userdata":                          {def: "m_no_attached_userdata_first_pool.json"},

		// Second pool cases
		"User dataset on other pool":                       {def: "m_with_userdata_on_other_pool.json", predictableSuffixFor: "rpool2/USERDATA/userfoo"},
		"User dataset with no user on other pool":          {def: "m_with_userdata_only_on_other_pool.json", predictableSuffixFor: "rpool2/USERDATA/userfoo"},
		"Prefer system pool for userdata":                  {def: "m_without_userdata_prefer_system_pool.json", predictableSuffixFor: "rpool/USERDATA/userfoo"},
		"Prefer system pool (try other pool) for userdata": {def: "m_without_userdata_prefer_system_pool.json", predictableSuffixFor: "rpool2/USERDATA/userfoo", cmdline: generateCmdLine("rpool2/ROOT/ubuntu_1234")},
		"No attached userdata on second pool":              {def: "m_no_attached_userdata_second_pool.json", predictableSuffixFor: "rpool2/USERDATA/userfoo"},

		// User or home edge cases
		"No user set":                                           {def: "m_with_userdata.json", user: "[empty]", wantErr: true, isNoOp: true},
		"No home path set":                                      {def: "m_with_userdata.json", homePath: "[empty]", wantErr: true, isNoOp: true},
		"User already exists on this machine":                   {def: "m_with_userdata.json", user: "user1"},
		"Target directory already exists and match user":        {def: "m_with_userdata.json", user: "user1", homePath: "/home/user1", isNoOp: true},
		"Target directory already exists and don't match user":  {def: "m_with_userdata.json", homePath: "/home/user1", wantErr: true, isNoOp: true},
		"Set Property when user already exists on this machine": {def: "m_with_userdata.json", setPropertyErr: true, user: "user1", wantErr: true, isNoOp: true},
		"Scan when user already exists fails":                   {def: "m_with_userdata.json", scanErr: true, user: "user1", isNoOp: true},

		// Error cases
		"System not zsys":                                              {def: "m_with_userdata_no_zsys.json", wantErr: true, isNoOp: true},
		"Create user dataset fails":                                    {def: "m_with_userdata.json", createErr: true, wantErr: true, isNoOp: true},
		"Create user dataset container fails":                          {def: "m_without_userdata.json", createErr: true, wantErr: true, isNoOp: true},
		"System bootfs property fails":                                 {def: "m_with_userdata.json", setPropertyErr: true, wantErr: true, isNoOp: true},
		"Scan for user dataset container fails":                        {def: "m_without_userdata.json", scanErr: true, wantErr: true, isNoOp: true},
		"Final scan fails issue warning and returns same machine list": {def: "m_with_userdata.json", scanErr: true, isNoOp: true},
	}

	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ds := machines.LoadDatasets(t, tc.def)
			tc.cmdline = getDefaultValue(tc.cmdline, generateCmdLine("rpool/ROOT/ubuntu_1234"))
			z := NewZfsMock(ds, "", getDefaultValue(tc.predictableSuffixFor, "rpool/USERDATA/userfoo"), tc.createErr, false, tc.scanErr, tc.setPropertyErr, false)
			initMachines := machines.New(context.Background(), ds, tc.cmdline)
			ms := initMachines

			err := ms.CreateUserData(z, getDefaultValue(tc.user, "userfoo"), getDefaultValue(tc.homePath, "/home/foo"))
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
			if z.scanErr {
				return
			}
			datasets, err := z.Scan()
			if err != nil {
				t.Fatal("couldn't rescan before checking final state:", err)
			}
			machinesAfterRescan := machines.New(context.Background(), datasets, tc.cmdline)
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
		"Rename home": {def: "m_with_userdata.json"},

		// Argument or matching issue
		"Home doesn't match": {def: "m_with_userdata.json", home: "/home/userabcd", wantErr: true, isNoOp: true},
		"System not zsys":    {def: "m_with_userdata_no_zsys.json", wantErr: true, isNoOp: true},
		"Old home empty":     {def: "m_with_userdata.json", home: "[empty]", wantErr: true, isNoOp: true},
		"New home empty":     {def: "m_with_userdata.json", newHome: "[empty]", wantErr: true, isNoOp: true},

		// Errors
		"Set property fails":               {def: "m_with_userdata.json", setPropertyErr: true, wantErr: true, isNoOp: true},
		"Scan fails doesn't trigger error": {def: "m_with_userdata.json", scanErr: true, isNoOp: true},
	}

	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ds := machines.LoadDatasets(t, tc.def)
			z := NewZfsMock(ds, "", "", false, false, tc.scanErr, tc.setPropertyErr, false)
			initMachines := machines.New(context.Background(), ds, generateCmdLine("rpool/ROOT/ubuntu_1234"))
			ms := initMachines

			err := ms.ChangeHomeOnUserData(z, getDefaultValue(tc.home, "/home/user1"), getDefaultValue(tc.newHome, "/home/foo"))
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
			if z.scanErr {
				return
			}
			datasets, err := z.Scan()
			if err != nil {
				t.Fatal("couldn't rescan before checking final state:", err)
			}
			machinesAfterRescan := machines.New(context.Background(), datasets, generateCmdLine("rpool/ROOT/ubuntu_1234"))
			assertMachinesEquals(t, machinesAfterRescan, ms)
		})
	}
}

func BenchmarkNewDesktop(b *testing.B) {
	ds := machines.LoadDatasets(b, "m_layout1_machines_with_snapshots_clones.json")
	config.SetVerboseMode(0)
	defer func() { config.SetVerboseMode(1) }()
	for n := 0; n < b.N; n++ {
		machines.New(context.Background(), ds, generateCmdLine("rpool/ROOT/ubuntu_5678"))
	}
}

func BenchmarkNewServer(b *testing.B) {
	ds := machines.LoadDatasets(b, "m_layout2_machines_with_snapshots_clones.json")
	config.SetVerboseMode(0)
	defer func() { config.SetVerboseMode(1) }()
	for n := 0; n < b.N; n++ {
		machines.New(context.Background(), ds, generateCmdLine("rpool/ROOT/ubuntu_5678"))
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

	if diff := cmp.Diff(m1, m2, cmpopts.EquateEmpty(),
		cmp.AllowUnexported(machines.Machines{}, zfs.DatasetProp{})); diff != "" {
		t.Errorf("Machines mismatch (-want +got):\n%s", diff)
	}
}

// assertMachinesNotEquals ensure that two machines are differents
func assertMachinesNotEquals(t *testing.T, m1, m2 machines.Machines) {
	t.Helper()

	if diff := cmp.Diff(m1, m2, cmpopts.EquateEmpty(),
		cmp.AllowUnexported(machines.Machines{}, zfs.DatasetProp{})); diff == "" {
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
