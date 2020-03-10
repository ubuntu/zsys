package machines

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/testutils"
	"github.com/ubuntu/zsys/internal/zfs"
	"github.com/ubuntu/zsys/internal/zfs/libzfs/mock"
)

func init() {
	config.SetVerboseMode(1)
}

func TestResolveOrigin(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		def              string
		onlyOnMountpoint string
	}{
		"one dataset":                                 {def: "d_one_machine_one_dataset.yaml"},
		"one machine with one snapshot":               {def: "d_one_machine_with_one_snapshot.yaml"},
		"one machine with one snapshot and one clone": {def: "d_one_machine_with_clone_dataset.yaml"},
		"one machine with multiple snapshots and clones, with chain of dependency":           {def: "d_one_machine_with_multiple_clones_recursive.yaml"},
		"one machine with multiple snapshots and clones, with chain of unordered dependency": {def: "d_one_machine_with_multiple_clones_recursive_unordered.yaml"},
		"one machine with children": {def: "d_one_machine_with_children.yaml"},
		"two machines":              {def: "d_two_machines_one_dataset.yaml"},

		// More real systems
		"Real machine, no snapshot, no clone":       {def: "m_layout1_one_machine.yaml"},
		"Real machines with snapshots and clones":   {def: "m_layout1_machines_with_snapshots_clones.yaml"},
		"Server machine, no snapshot, no clone":     {def: "m_layout2_one_machine.yaml"},
		"Server machines with snapshots and clones": {def: "m_layout2_machines_with_snapshots_clones.yaml"},

		"Select master only":                                {def: "d_one_machine_with_multiple_clones_recursive_with_chilren.yaml", onlyOnMountpoint: "/"},
		"Select a particular mountpoint":                    {def: "d_one_machine_with_multiple_clones_recursive_with_chilren.yaml", onlyOnMountpoint: "/child"},
		"Select no matching dataset mountpoints":            {def: "d_one_machine_with_multiple_clones_recursive_with_chilren.yaml", onlyOnMountpoint: "/none"},
		"Select all datasets without selecting mountpoints": {def: "d_one_machine_with_multiple_clones_recursive_with_chilren.yaml", onlyOnMountpoint: "-"},

		// Failing cases
		// NOTE: This case cannot happen and cannot be represented in the yaml test data
		//"missing clone referenced by a snapshot clone (broken ZFS)": {def: "d_one_machine_missing_clone.yaml"},
		"no dataset":                 {def: "d_no_dataset.yaml"},
		"no interesting mountpoints": {def: "d_no_machine.yaml"},
	}

	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			dir, cleanup := testutils.TempDir(t)
			defer cleanup()

			libzfs := testutils.GetMockZFS(t)
			fPools := testutils.NewFakePools(t, filepath.Join("testdata", tc.def), testutils.WithLibZFS(libzfs))
			defer fPools.Create(dir)()

			z, err := zfs.New(context.Background(), zfs.WithLibZFS(libzfs))
			if err != nil {
				t.Fatalf("couldn’t create original zfs datasets state")
			}

			var ds []*zfs.Dataset
			for _, d := range z.Datasets() {
				d := d
				ds = append(ds, d)
			}
			if tc.onlyOnMountpoint == "" {
				tc.onlyOnMountpoint = "/"
			}
			if tc.onlyOnMountpoint == "-" {
				tc.onlyOnMountpoint = ""
			}

			got := resolveOrigin(context.Background(), ds, tc.onlyOnMountpoint)

			assertDatasetsOrigin(t, got)
		})
	}
}

func TestGetDependencies(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		def       string
		stateName string

		wantStates   []string
		wantDatasets []string
	}{
		"Simple leaf state":                        {def: "m_clone_simple.yaml", stateName: "rpool/ROOT/ubuntu_5678", wantStates: []string{"rpool/ROOT/ubuntu_5678"}},
		"Simple leaf state with children datasets": {def: "m_clone_with_children.yaml", stateName: "rpool/ROOT/ubuntu_5678", wantStates: []string{"rpool/ROOT/ubuntu_5678"}},
		"Simple snapshot state": {def: "m_clone_simple.yaml", stateName: "rpool/ROOT/ubuntu_1234@snap1",
			wantStates: []string{"rpool/ROOT/ubuntu_5678", "rpool/ROOT/ubuntu_1234@snap1"}},
		"Simple snapshot state with children datasets": {def: "m_clone_with_children.yaml", stateName: "rpool/ROOT/ubuntu_1234@snap1",
			wantStates: []string{"rpool/ROOT/ubuntu_5678", "rpool/ROOT/ubuntu_1234@snap1"}},
		"Simple state with clone": {def: "m_clone_simple.yaml", stateName: "rpool/ROOT/ubuntu_1234",
			wantStates: []string{"rpool/ROOT/ubuntu_5678", "rpool/ROOT/ubuntu_1234@snap1", "rpool/ROOT/ubuntu_1234"}},
		"Simple state with clone with children datasets": {def: "m_clone_with_children.yaml", stateName: "rpool/ROOT/ubuntu_1234",
			wantStates: []string{"rpool/ROOT/ubuntu_5678", "rpool/ROOT/ubuntu_1234@snap1", "rpool/ROOT/ubuntu_1234"}},

		// State with clone dataset
		"Simple state with manual clone": {def: "m_clone_simple_with_manual_clone.yaml", stateName: "rpool/ROOT/ubuntu_1234",
			wantStates:   []string{"rpool/ROOT/ubuntu_1234@snap1", "rpool/ROOT/ubuntu_1234"},
			wantDatasets: []string{"rpool/ROOT/ubuntu_manual"},
		},

		// System states with user states
		"Leaf system state with userdata": {def: "m_clone_with_userdata.yaml", stateName: "rpool/ROOT/ubuntu_5678",
			wantStates: []string{"rpool/USERDATA/user1_efgh", "rpool/ROOT/ubuntu_5678"}},
		"System state with clone with userdata (less users on clone -> user has been created)": {def: "m_clone_with_userdata.yaml", stateName: "rpool/ROOT/ubuntu_1234",
			wantStates: []string{"rpool/USERDATA/root_bcde", "rpool/USERDATA/user1_efgh", "rpool/USERDATA/user1_abcd@snap1", "rpool/USERDATA/user1_abcd",
				"rpool/ROOT/ubuntu_5678", "rpool/ROOT/ubuntu_1234@snap1", "rpool/ROOT/ubuntu_1234"}},
		"System state with clone with userdata (more users on clone -> user has been deleted)": {def: "m_clone_with_clone_has_more_users.yaml", stateName: "rpool/ROOT/ubuntu_1234",
			wantStates: []string{"rpool/USERDATA/user1_efgh", "rpool/USERDATA/user1_abcd@snap1", "rpool/USERDATA/user1_abcd", "rpool/USERDATA/root_bcde",
				"rpool/ROOT/ubuntu_5678", "rpool/ROOT/ubuntu_1234@snap1", "rpool/ROOT/ubuntu_1234"}},

		// Multiclones
		"Leaf user clone with snapshots": {def: "state_snapshot_with_userdata_n_clones.yaml", stateName: "rpool/USERDATA/user1_mnop",
			wantStates: []string{"rpool/USERDATA/user1_mnop@snapuser3", "rpool/USERDATA/user1_mnop"}},
		"Root user with mutiple clones and system - user snapshots": {def: "state_snapshot_with_userdata_n_clones.yaml", stateName: "rpool/USERDATA/user1_abcd",
			wantStates: []string{"rpool/USERDATA/user1_abcd@snap3", "rpool/USERDATA/user1_abcd@snapuser1",
				"rpool/USERDATA/user1_efgh@snapuser5", "rpool/USERDATA/user1_uvwx@snapuser4", "rpool/USERDATA/user1_uvwx",
				"rpool/USERDATA/user1_qrst@snapuser3", "rpool/USERDATA/user1_qrst", "rpool/USERDATA/user1_mnop@snapuser3",
				"rpool/USERDATA/user1_mnop", "rpool/USERDATA/user1_ijkl", "rpool/USERDATA/user1_efgh@snapuser2",
				"rpool/USERDATA/user1_efgh@snapuser3", "rpool/USERDATA/user1_efgh", "rpool/USERDATA/user1_abcd@snap2",
				"rpool/USERDATA/user1_abcd@snap1", "rpool/USERDATA/user1_abcd"}},
		"Root user with mutiple clones and system - user snapshots with manual clone datasets": {def: "state_snapshot_with_userdata_n_clones_linked_datasets.yaml", stateName: "rpool/USERDATA/user1_abcd",
			wantStates: []string{"rpool/USERDATA/user1_abcd@snap3", "rpool/USERDATA/user1_abcd@snapuser1",
				"rpool/USERDATA/user1_efgh@snapuser5", "rpool/USERDATA/user1_uvwx@snapuser4", "rpool/USERDATA/user1_uvwx",
				"rpool/USERDATA/user1_qrst@snapuser3", "rpool/USERDATA/user1_qrst", "rpool/USERDATA/user1_mnop@snapuser3",
				"rpool/USERDATA/user1_mnop", "rpool/USERDATA/user1_ijkl", "rpool/USERDATA/user1_efgh@snapuser2",
				"rpool/USERDATA/user1_efgh@snapuser3", "rpool/USERDATA/user1_efgh", "rpool/USERDATA/user1_abcd@snap2",
				"rpool/USERDATA/user1_abcd@snap1", "rpool/USERDATA/user1_abcd"},
			wantDatasets: []string{"rpool/user1_xyz", "rpool/user1_aaaa"}},
		"Root system with mutiple clones and bpool": {def: "state_snapshot_with_userdata_n_system_clones.yaml", stateName: "rpool/ROOT/ubuntu_1234",
			wantStates: []string{"rpool/USERDATA/root_bcde@snap1",
				"rpool/ROOT/ubuntu_1234@snap1",
				"rpool/USERDATA/root_cdef@snaproot1", "rpool/USERDATA/root_cdef@snap4", "rpool/USERDATA/root_cdef", "rpool/USERDATA/root_bcde@snap2",
				"rpool/ROOT/ubuntu_1234@snap2",
				"rpool/USERDATA/root_ghij", "rpool/USERDATA/root_defg@snaproot2", "rpool/USERDATA/root_defg",
				"rpool/ROOT/ubuntu_9999", "rpool/ROOT/ubuntu_5678@snap4", "rpool/ROOT/ubuntu_5678",
				"rpool/USERDATA/root_bcde@snap3",
				"rpool/ROOT/ubuntu_1234@snap3",
				"rpool/USERDATA/root_bcde", "rpool/USERDATA/user1_abcd",
				"rpool/ROOT/ubuntu_1234"}},
		"Root system with mutiple clones and bpool and manual clone": {def: "state_snapshot_with_userdata_n_system_clones_manual_clone.yaml", stateName: "rpool/ROOT/ubuntu_1234",
			wantStates: []string{"rpool/USERDATA/root_bcde@snap1",
				"rpool/ROOT/ubuntu_1234@snap1",
				"rpool/USERDATA/root_cdef@snaproot1", "rpool/USERDATA/root_cdef@snap4", "rpool/USERDATA/root_cdef", "rpool/USERDATA/root_bcde@snap2",
				"rpool/ROOT/ubuntu_1234@snap2",
				"rpool/USERDATA/root_ghij", "rpool/USERDATA/root_defg@snaproot2", "rpool/USERDATA/root_defg",
				"rpool/ROOT/ubuntu_9999", "rpool/ROOT/ubuntu_5678@snap4", "rpool/ROOT/ubuntu_5678",
				"rpool/USERDATA/root_bcde@snap3",
				"rpool/ROOT/ubuntu_1234@snap3",
				"rpool/USERDATA/root_bcde", "rpool/USERDATA/user1_abcd",
				"rpool/ROOT/ubuntu_1234"},
			wantDatasets: []string{"rpool/manualclone"}},

		// User state linked to a system state: we never list the associated system state (complexity outbreak), it will be treated by the caller
		"Leaf user state doesn’t list its system state": {def: "m_clone_with_userdata.yaml", stateName: "rpool/USERDATA/user1_efgh",
			wantStates: []string{"rpool/USERDATA/user1_efgh"}},
		"User state with clone doesn’t list corresponding system states and other user states": {def: "m_clone_with_userdata.yaml", stateName: "rpool/USERDATA/user1_abcd",
			wantStates: []string{"rpool/USERDATA/user1_efgh", "rpool/USERDATA/user1_abcd@snap1", "rpool/USERDATA/user1_abcd"}},
	}

	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			dir, cleanup := testutils.TempDir(t)
			defer cleanup()

			libzfs := testutils.GetMockZFS(t)
			fPools := testutils.NewFakePools(t, filepath.Join("testdata", tc.def), testutils.WithLibZFS(libzfs))
			defer fPools.Create(dir)()

			_, err := zfs.New(context.Background(), zfs.WithLibZFS(libzfs))
			if err != nil {
				t.Fatalf("couldn’t create original zfs datasets state")
			}

			ms, err := New(context.Background(), "", WithLibZFS(libzfs))
			if err != nil {
				t.Error("expected success but got an error scanning for machines", err)
			}
			s := ms.getStateFromName(t, tc.stateName)

			stateDeps, datasetDeps := s.getDependencies(&ms)

			stateNames := make([]string, len(stateDeps))
			for i, s := range stateDeps {
				stateNames[i] = s.ID
			}
			datasetNames := make([]string, len(datasetDeps))
			for i, d := range datasetDeps {
				datasetNames[i] = d.Name
			}

			// Ensure that the 2 lists have the same elements
			if len(stateDeps) != len(tc.wantStates) {
				t.Errorf("states content doesn't have enough elements:\nGot:  %v\nWant: %v", stateNames, tc.wantStates)
			} else {
				assert.ElementsMatch(t, tc.wantStates, stateNames, "didn't get matching states list content")
			}
			if len(datasetDeps) != len(tc.wantDatasets) {
				t.Errorf("dataset deps content doesn't have enough elements:\nGot:  %v\nWant: %v", datasetNames, tc.wantDatasets)
			} else {
				assert.ElementsMatch(t, tc.wantDatasets, datasetNames, "didn't get matching dep list content")
			}
		})
	}
}

func TestParentSystemTest(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		stateName string

		wantState string
	}{
		"User reports parent active system state":           {stateName: "rpool/USERDATA/user1_abcd", wantState: "rpool/ROOT/ubuntu_1234"},
		"User reports parent history clone system state":    {stateName: "rpool/USERDATA/user1_efgh", wantState: "rpool/ROOT/ubuntu_5678"},
		"User reports parent history snapshot system state": {stateName: "rpool/USERDATA/user1_abcd@snap1", wantState: "rpool/ROOT/ubuntu_1234@snap1"},

		"User reports no parent for user snapshot": {stateName: "rpool/USERDATA/user1_abcd@snapuser1"},

		"Report nothing on system active system state":  {stateName: "rpool/ROOT/ubuntu_1234"},
		"Report nothing on system history system state": {stateName: "rpool/ROOT/ubuntu_5678"},
	}
	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			dir, cleanup := testutils.TempDir(t)
			defer cleanup()

			libzfs := testutils.GetMockZFS(t)
			fPools := testutils.NewFakePools(t, filepath.Join("testdata", "state_snapshot_with_userdata_n_clones.yaml"), testutils.WithLibZFS(libzfs))
			defer fPools.Create(dir)()

			_, err := zfs.New(context.Background(), zfs.WithLibZFS(libzfs))
			if err != nil {
				t.Fatalf("couldn’t create original zfs datasets state")
			}

			ms, err := New(context.Background(), "", WithLibZFS(libzfs))
			if err != nil {
				t.Error("expected success but got an error scanning for machines", err)
			}
			s := ms.getStateFromName(t, tc.stateName)

			got := s.parentSystemState(&ms)

			if got == nil {
				if tc.wantState != "" {
					t.Fatalf("Got a nil state when expecting %s", tc.wantState)
				}
				return
			} else if tc.wantState == "" {
				t.Fatalf("Expected nil state but got %s", got.ID)
			}

			assert.Equal(t, tc.wantState, got.ID, "didn't get expected state")
		})
	}
}

/*
FIXME 2020-03-09: This case fails with the current data structure.
Case of a user state attached to 2 different machines, one of the machine has a subdataset.
In this case, the state with the same ID is added twice to allUsersDatasets.

This is described by the following test definition:

pools:
  - name: rpool
    datasets:
    - name: ROOT
    - name: ROOT/ubuntu_1234
[...]
    - name: ROOT/ubuntu_5678
[...]
    - name: ROOT/ubuntu_1234/opt
[...]
    - name: ROOT/ubuntu_5678/opt
      origin: rpool/ROOT/ubuntu_1234/opt@snap2
    - name: USERDATA
      canmount: off
    - name: USERDATA/user1_abcd
[...]
    - name: USERDATA/root_bcde
    - name: USERDATA/root_bcde/subfor5678
      mountpoint: /root/subfor5678
      last_used: 2018-08-03T21:55:33+00:00
      bootfs_datasets: rpool/ROOT/ubuntu_5678
      snapshots:
        - name: snap2
          mountpoint: /root/subfor5678:inherited
          canmount: on:local
          creation_time: 2019-04-18T02:45:55+00:00

*/
func TestRemoveInternal(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		stateName               string
		linkedStateID           string
		dontRemoveUsersChildren bool

		setPropertyErr bool

		wantErr bool
		isNoOp  bool
	}{
		"Initial state":                                 {}, // This state doesn’t call remove at all but is used to compare golden files
		"Remove user state unconditionnally":            {stateName: "rpool/USERDATA/user2_2222"},
		"Remove user state conditionnally":              {stateName: "rpool/USERDATA/user2_2222", linkedStateID: "rpool/ROOT/ubuntu_5678"},
		"Don't remove user state on bad state id match": {stateName: "rpool/USERDATA/user2_2222", linkedStateID: "doesnt match"},

		"Remove user state unconditionnally linked to 2 states":                   {stateName: "rpool/USERDATA/user3_3333"},
		"Remove user state unconditionnally linked to 2 states and its snapshots": {stateName: "rpool/USERDATA/root_bcde"},
		"Unassociate user state linked to one state":                              {stateName: "rpool/USERDATA/user3_3333", linkedStateID: "rpool/ROOT/ubuntu_1234"},

		"Remove user snapshot state":                                    {stateName: "rpool/USERDATA/root_bcde@snaproot1"},
		"Remove user snapshot state (linked to system state: no check)": {stateName: "rpool/USERDATA/root_bcde@snap2"},
		"Remove user snapshot clone state":                              {stateName: "rpool/USERDATA/user4_clone"},

		"Remove system state without user datasets": {stateName: "rpool/ROOT/ubuntu_6789"},
		"Remove system state and its user datasets": {stateName: "rpool/ROOT/ubuntu_5678"},

		// Error on clones from state
		"Error on removing state with state clone":   {stateName: "rpool/USERDATA/user4_for_state_clone", wantErr: true},
		"Error on removing state with dataset clone": {stateName: "rpool/USERDATA/user5_for_manual_clone", wantErr: true},

		"Revert unassociate user state if we get an error":          {stateName: "rpool/USERDATA/user2_2222", linkedStateID: "rpool/ROOT/ubuntu_5678", setPropertyErr: true, wantErr: true, isNoOp: true},
		"Don’t destroy system state if user remove issues an error": {stateName: "rpool/ROOT/ubuntu_5678", setPropertyErr: true, wantErr: true, isNoOp: true},
	}
	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			dir, cleanup := testutils.TempDir(t)
			defer cleanup()

			libzfs := testutils.GetMockZFS(t)
			fPools := testutils.NewFakePools(t, filepath.Join("testdata", "state_remove_internal.yaml"), testutils.WithLibZFS(libzfs))
			defer fPools.Create(dir)()

			lzfs := libzfs.(*mock.LibZFS)
			lzfs.ErrOnSetProperty(tc.setPropertyErr)

			ms, err := New(context.Background(), "", WithLibZFS(libzfs))
			if err != nil {
				t.Error("expected success but got an error scanning for machines", err)
			}

			initMachines := ms.CopyForTests(t)

			// Create init file for comparison
			if tc.stateName == "" {
				assertMachinesToGolden(t, ms)
				return
			}

			s := ms.getStateFromName(t, tc.stateName)

			err = s.remove(context.Background(), ms.z, tc.linkedStateID, tc.dontRemoveUsersChildren)
			if err != nil {
				if !tc.wantErr {
					t.Fatalf("expected no error but got: %v", err)
				}
				return
			}
			if err == nil && tc.wantErr {
				t.Fatal("expected an error but got none")
			}

			got, err := New(context.Background(), "", WithLibZFS(libzfs))
			if err != nil {
				t.Error("expected success but got an error scanning for machines", err)
			}
			if tc.isNoOp {
				assertMachinesEquals(t, initMachines, got)
				return
			}

			assertMachinesToGolden(t, got)
		})
	}
}

// assertDatasetsOrigin compares got maps of origin to reference files, based on test name.
func assertDatasetsOrigin(t *testing.T, got map[string]*string) {
	want := make(map[string]*string)
	testutils.LoadFromGoldenFile(t, got, &want)

	if diff := cmp.Diff(want, got, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("Dataset origin mismatch (-want +got):\n%s", diff)
	}
}

// assertMachinesToGolden compares got slice of machines to reference files, based on test name.
func assertMachinesToGolden(t *testing.T, got Machines) {
	t.Helper()

	want := Machines{}
	got.MakeComparable()
	testutils.LoadFromGoldenFile(t, got, &want)

	assertMachinesEquals(t, want, got)
}

// assertMachinesEquals compares two machines
func assertMachinesEquals(t *testing.T, m1, m2 Machines) {
	t.Helper()

	m1.MakeComparable()
	m2.MakeComparable()

	if diff := cmp.Diff(m1, m2, cmpopts.EquateEmpty(),
		cmp.AllowUnexported(Machines{}),
		cmpopts.IgnoreUnexported(zfs.Dataset{}, zfs.DatasetProp{})); diff != "" {
		t.Errorf("Machines mismatch (-want +got):\n%s", diff)
	}
}

func (ms *Machines) getStateFromName(t *testing.T, name string) *State {
	t.Helper()

	var s *State
foundState:
	for _, m := range ms.all {
		if name == m.ID {
			s = &m.State
			break
		}
		for _, h := range m.History {
			if name == h.ID {
				s = h
				break foundState
			}
		}
		for _, aus := range m.AllUsersStates {
			for _, us := range aus {
				if name == us.ID {
					s = us
					break foundState
				}
			}
		}
	}

	if s == nil {
		t.Fatalf("No state found matching %s", name)
	}
	return s
}
