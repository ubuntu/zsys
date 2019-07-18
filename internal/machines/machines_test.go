package machines_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/machines"
	"github.com/ubuntu/zsys/internal/testutils"
	"github.com/ubuntu/zsys/internal/zfs"
)

const cmdLineLayout1And2 = "aaaaa bbbbb root=ZFS=rpool/ROOT/ubuntu_5678 ccccc"

func init() {
	testutils.InstallUpdateFlag()
	config.SetVerboseMode(true)
}

func TestNew(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		def     string
		cmdline string
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
		"Select existing dataset machine":           {def: "d_one_machine_one_dataset.json", cmdline: "aaaaa bbbbb root=ZFS=rpool ccccc"},
		"Select correct machine":                    {def: "d_two_machines_one_dataset.json", cmdline: "aaaaa bbbbb root=ZFS=rpool2 ccccc"},
		"Select main machine with snapshots/clones": {def: "m_clone_with_persistent.json", cmdline: "aaaaa bbbbb root=ZFS=rpool/ROOT/ubuntu_1234 ccccc"},
		"Select snapshot":                           {def: "m_clone_with_persistent.json", cmdline: "aaaaa bbbbb root=ZFS=rpool/ROOT/ubuntu_1234@snap1 ccccc"},
		"Select clone":                              {def: "m_clone_with_persistent.json", cmdline: "aaaaa bbbbb root=ZFS=rpool/ROOT/ubuntu_5678 ccccc"},
		"Selected machine doesn't exist":            {def: "d_one_machine_one_dataset.json", cmdline: "aaaaa bbbbb root=ZFS=foo ccccc"},
		"Select existing dataset but not a machine": {def: "m_with_persistent.json", cmdline: "aaaaa bbbbb root=ZFS=rpool/ROOT ccccc"},
	}

	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ds := machines.LoadDatasets(t, tc.def)

			got := machines.New(ds, tc.cmdline)

			assertMachinesToGolden(t, got)
		})
	}
}

func TestIdempotentNew(t *testing.T) {
	t.Parallel()
	ds := machines.LoadDatasets(t, "m_layout2_machines_with_snapshots_clones.json")

	got1 := machines.New(ds, cmdLineLayout1And2)
	got2 := machines.New(ds, cmdLineLayout1And2)

	assertMachinesEquals(t, got1, got2)
}

	}
}

func BenchmarkNewDesktop(b *testing.B) {
	ds := machines.LoadDatasets(b, "m_layout1_machines_with_snapshots_clones.json")
	config.SetVerboseMode(false)
	defer func() { config.SetVerboseMode(true) }()
	for n := 0; n < b.N; n++ {
		machines.New(ds, cmdLineLayout1And2)
	}
}

func BenchmarkNewServer(b *testing.B) {
	ds := machines.LoadDatasets(b, "m_layout2_machines_with_snapshots_clones.json")
	config.SetVerboseMode(false)
	defer func() { config.SetVerboseMode(true) }()
	for n := 0; n < b.N; n++ {
		machines.New(ds, cmdLineLayout1And2)
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
