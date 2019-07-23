package machines_test

import (
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
	config.SetVerboseMode(true)
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

			got := machines.New(ds, tc.cmdline)

			assertMachinesToGolden(t, got)
		})
	}
}

func TestIdempotentNew(t *testing.T) {
	t.Parallel()
	ds := machines.LoadDatasets(t, "m_layout2_machines_with_snapshots_clones.json")

	got1 := machines.New(ds, generateCmdLine("rpool/ROOT/ubuntu_5678"))
	got2 := machines.New(ds, generateCmdLine("rpool/ROOT/ubuntu_5678"))

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
			z := NewZfsMock(ds, tc.mountedDataset, tc.predictableSuffixFor, tc.cloneErr, tc.scanErr, tc.setPropertyErr, false)
			// Reload datasets from zfs, as mountedDataset can change .Mounted property (reused on consecutive Scan())
			var datasets []zfs.Dataset
			for _, d := range z.d {
				datasets = append(datasets, *d)
			}
			initMachines := machines.New(datasets, tc.cmdline)
			ms := initMachines

			err := ms.EnsureBoot(z, tc.cmdline)
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

			datasets, err = z.Scan()
			if err != nil {
				t.Fatal("couldn't rescan before checking final state:", err)
			}
			machinesAfterRescan := machines.New(datasets, tc.cmdline)
			assertMachinesEquals(t, machinesAfterRescan, ms)
		})
	}
}

func TestIdempotentBoot(t *testing.T) {
	t.Parallel()
	ds := machines.LoadDatasets(t, "m_layout2_machines_with_snapshots_clones_reverting.json")
	cmdline := generateCmdLineWithRevert("rpool/ROOT/ubuntu_5678")
	z := NewZfsMock(ds, "rpool/ROOT/ubuntu_4242", "rpool/USERDATA/user1", false, false, false, false)
	datasets, err := z.Scan()
	if err != nil {
		t.Fatal("couldn't scan for initial state:", err)
	}
	ms1 := machines.New(datasets, cmdline)

	if err = ms1.EnsureBoot(z, cmdline); err != nil {
		t.Fatal("First EnsureBoot failed:", err)
	}
	ms2 := ms1
	if err = ms2.EnsureBoot(z, cmdline); err != nil {
		t.Fatal("Second EnsureBoot failed:", err)
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
	cmdline := generateCmdLineWithRevert("rpool/ROOT/ubuntu_5678@snap3")
	z := NewZfsMock(ds, "rpool/ROOT/ubuntu_4242", "rpool/USERDATA/user1", false, false, false, false)
	datasets, err := z.Scan()
	if err != nil {
		t.Fatal("couldn't scan for initial state:", err)
	}
	ms1 := machines.New(datasets, cmdline)

	if err = ms1.EnsureBoot(z, cmdline); err != nil {
		t.Fatal("First EnsureBoot failed:", err)
	}
	if err = ms1.Commit(z, cmdline); err != nil {
		t.Fatal("Commit failed:", err)
	}

	ms2 := ms1
	if err = ms2.EnsureBoot(z, cmdline); err != nil {
		t.Fatal("Second EnsureBoot failed:", err)
	}

	assertMachinesEquals(t, ms1, ms2)
}

func TestIdempotentBootSnapshotBeforeCommit(t *testing.T) {
	t.Parallel()
	ds := machines.LoadDatasets(t, "m_layout2_machines_with_snapshots_clones_reverting.json")
	cmdline := generateCmdLineWithRevert("rpool/ROOT/ubuntu_5678@snap3")
	z := NewZfsMock(ds, "rpool/ROOT/ubuntu_4242", "rpool/USERDATA/user1", false, false, false, false)
	datasets, err := z.Scan()
	if err != nil {
		t.Fatal("couldn't scan for initial state:", err)
	}
	ms1 := machines.New(datasets, cmdline)

	if err = ms1.EnsureBoot(z, cmdline); err != nil {
		t.Fatal("First EnsureBoot failed:", err)
	}
	ms2 := ms1
	if err = ms2.EnsureBoot(z, cmdline); err != nil {
		t.Fatal("Second EnsureBoot failed:", err)
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

		wantErr bool
		isNoOp  bool
	}{
		"One machine, commit one clone": {def: "d_one_machine_with_clone_to_promote.json", cmdline: generateCmdLine("rpool_clone")},
		"One machine, commit current":   {def: "d_one_machine_with_clone_dataset.json", cmdline: generateCmdLine("rpool")},
		"One machine non zsys":          {def: "d_one_machine_one_dataset_non_zsys.json", cmdline: generateCmdLine("rpool"), isNoOp: true},
		"One machine no match":          {def: "d_one_machine_one_dataset.json", cmdline: generateCmdLine("rpoolfake"), isNoOp: true},

		"One machine with children": {def: "m_clone_with_children_to_promote.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678")},
		"Without suffix":            {def: "m_main_dataset_without_suffix_and_clone_to_promote.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu")},

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
		"SetProperty fails":      {def: "m_clone_with_userdata_to_promote_no_user_revert.json", cmdline: generateCmdLine("rpool/ROOT/ubuntu_5678"), setPropertyErr: true, wantErr: true},
		"Promote fails":          {def: "d_one_machine_with_clone_dataset.json", cmdline: generateCmdLine("rpool_clone"), promoteErr: true, wantErr: true},
		"Promote userdata fails": {def: "m_clone_with_userdata_to_promote_user_revert.json", cmdline: generateCmdLineWithRevert("rpool/ROOT/ubuntu_5678"), promoteErr: true, wantErr: true},
		"Scan fails":             {def: "d_one_machine_with_clone_dataset.json", cmdline: generateCmdLine("rpool"), scanErr: true, wantErr: true},
	}
	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ds := machines.LoadDatasets(t, tc.def)
			z := NewZfsMock(ds, "", "", false, tc.scanErr, tc.setPropertyErr, tc.promoteErr)
			// Do the Scan() manually, as we don't wan't to suffer from scanErr
			var datasets []zfs.Dataset
			for _, d := range z.d {
				datasets = append(datasets, *d)
			}
			initMachines := machines.New(datasets, tc.cmdline)
			ms := initMachines

			err := ms.Commit(z, tc.cmdline)
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

			datasets, err = z.Scan()
			if err != nil {
				t.Fatal("couldn't rescan before checking final state:", err)
			}
			machinesAfterRescan := machines.New(datasets, tc.cmdline)
			assertMachinesEquals(t, machinesAfterRescan, ms)
		})
	}
}

func TestIdempotentCommit(t *testing.T) {
	t.Parallel()
	ds := machines.LoadDatasets(t, "m_layout2_machines_with_snapshots_clones_no_user_revert.json")
	z := NewZfsMock(ds, "", "", false, false, false, false)
	datasets, err := z.Scan()
	if err != nil {
		t.Fatal("couldn't scan for initial state:", err)
	}
	ms1 := machines.New(datasets, generateCmdLine("rpool/ROOT/ubuntu_9876"))

	if err = ms1.Commit(z, generateCmdLine("rpool/ROOT/ubuntu_9876")); err != nil {
		t.Fatal("first commit failed:", err)
	}
	ms2 := ms1
	if err = ms2.Commit(z, generateCmdLine("rpool/ROOT/ubuntu_9876")); err != nil {
		t.Fatal("second commit failed:", err)
	}

	assertMachinesEquals(t, ms1, ms2)
}

func BenchmarkNewDesktop(b *testing.B) {
	ds := machines.LoadDatasets(b, "m_layout1_machines_with_snapshots_clones.json")
	config.SetVerboseMode(false)
	defer func() { config.SetVerboseMode(true) }()
	for n := 0; n < b.N; n++ {
		machines.New(ds, generateCmdLine("rpool/ROOT/ubuntu_5678"))
	}
}

func BenchmarkNewServer(b *testing.B) {
	ds := machines.LoadDatasets(b, "m_layout2_machines_with_snapshots_clones.json")
	config.SetVerboseMode(false)
	defer func() { config.SetVerboseMode(true) }()
	for n := 0; n < b.N; n++ {
		machines.New(ds, generateCmdLine("rpool/ROOT/ubuntu_5678"))
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
func generateCmdLine(datasetName string) string {
	return "aaaaa bbbbb root=ZFS=" + datasetName + " ccccc"
}

// generateCmdLineWithRevert returns a command line with fake boot and a revert user data argument
func generateCmdLineWithRevert(datasetName string) string {
	return generateCmdLine(datasetName) + " " + machines.RevertUserDataTag
}
