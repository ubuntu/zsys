package machines

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/testutils"
	"github.com/ubuntu/zsys/internal/zfs"
	"github.com/ubuntu/zsys/internal/zfs/libzfs/mock"
)

func TestMain(m *testing.M) {
	vv := flag.Bool("vv", false, "More verbosity")
	flag.Parse()

	config.SetVerboseMode(1)
	if *vv {
		config.SetVerboseMode(2)
	}
	os.Exit(m.Run())
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

		// System states with user states don’t list user states
		"Leaf system state with userdata": {def: "m_clone_with_userdata.yaml", stateName: "rpool/ROOT/ubuntu_5678",
			wantStates: []string{"rpool/USERDATA/user1_efgh", "rpool/ROOT/ubuntu_5678"}},
		"System state with clone with userdata (less users on clone -> user has been created)": {def: "m_clone_with_userdata.yaml", stateName: "rpool/ROOT/ubuntu_1234",
			wantStates: []string{"rpool/USERDATA/user1_efgh", "rpool/ROOT/ubuntu_5678", "rpool/USERDATA/user1_abcd@snap1", "rpool/ROOT/ubuntu_1234@snap1", "rpool/USERDATA/root_bcde", "rpool/USERDATA/user1_abcd", "rpool/ROOT/ubuntu_1234"}},
		"System state with clone with userdata (more users on clone -> user has been deleted)": {def: "m_clone_with_clone_has_more_users.yaml", stateName: "rpool/ROOT/ubuntu_1234",
			wantStates: []string{
				"rpool/USERDATA/user1_efgh", "rpool/USERDATA/root_bcde", "rpool/ROOT/ubuntu_5678",
				"rpool/USERDATA/user1_abcd@snap1", "rpool/ROOT/ubuntu_1234@snap1",
				"rpool/USERDATA/user1_abcd", "rpool/ROOT/ubuntu_1234"}},

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
			wantStates: []string{
				"rpool/USERDATA/root_ghij",
				"rpool/USERDATA/root_defg@snaproot2",
				"rpool/USERDATA/root_defg",
				"rpool/ROOT/ubuntu_9999",
				"rpool/USERDATA/root_cdef@snap4",
				"rpool/ROOT/ubuntu_5678@snap4",
				"rpool/USERDATA/root_cdef@snaproot1",
				"rpool/USERDATA/root_cdef",
				"rpool/ROOT/ubuntu_5678",
				"rpool/USERDATA/root_bcde@snap3",
				"rpool/ROOT/ubuntu_1234@snap3",
				"rpool/USERDATA/root_bcde@snap2",
				"rpool/ROOT/ubuntu_1234@snap2",
				"rpool/USERDATA/root_bcde@snap1",
				"rpool/ROOT/ubuntu_1234@snap1",
				"rpool/USERDATA/root_bcde",
				"rpool/USERDATA/user1_abcd",
				"rpool/ROOT/ubuntu_1234"}},

		"Root system with mutiple clones and bpool and manual clone": {def: "state_snapshot_with_userdata_n_system_clones_manual_clone.yaml", stateName: "rpool/ROOT/ubuntu_1234",
			wantStates: []string{
				"rpool/USERDATA/root_bcde@snap1", "rpool/ROOT/ubuntu_1234@snap1",
				"rpool/USERDATA/root_cdef@snap4",
				"rpool/USERDATA/root_cdef@snaproot1", "rpool/USERDATA/root_cdef",
				"rpool/USERDATA/root_bcde@snap2", "rpool/ROOT/ubuntu_1234@snap2",
				"rpool/USERDATA/root_ghij",
				"rpool/USERDATA/root_defg@snaproot2",
				"rpool/USERDATA/root_defg", "rpool/ROOT/ubuntu_9999",
				"rpool/ROOT/ubuntu_5678@snap4",
				"rpool/ROOT/ubuntu_5678",
				"rpool/USERDATA/root_bcde@snap3", "rpool/ROOT/ubuntu_1234@snap3",
				"rpool/USERDATA/root_bcde", "rpool/USERDATA/user1_abcd", "rpool/ROOT/ubuntu_1234"},
			wantDatasets: []string{"rpool/manualclone"}},

		// User state linked to 2 machines has the user state object (different object) listed twice
		"User state linked to 2 machines": {def: "state_snapshot_with_userdata_n_clones.yaml", stateName: "rpool/ROOT/ubuntu_machine2clone",
			wantStates: []string{
				"rpool/USERDATA/user1_machine2", "rpool/ROOT/ubuntu_machine2clone"}},
		"User state linked to 2 machines, remove main machine": {def: "state_snapshot_with_userdata_n_clones.yaml", stateName: "rpool/ROOT/ubuntu_machine2",
			wantStates: []string{
				"rpool/USERDATA/user1_machine2", "rpool/ROOT/ubuntu_machine2clone",
				"rpool/ROOT/ubuntu_machine2@snapmachine2",
				// "rpool/USERDATA/user1_machine2" is another instance (which can have different route and subdatasets) than "rpool/USERDATA/user1_machine2"
				"rpool/USERDATA/user1_machine2", "rpool/ROOT/ubuntu_machine2",
			}},

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

			stateDeps, datasetDeps := s.getDependencies(context.Background(), &ms)

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

			// rule 2: ensure that all children (snapshots or filesystem datasets) appears before its parent
			assertChildrenStatesBeforeParents(t, stateDeps)
			assertChildrenBeforeParents(t, datasetDeps)

			// rule 3: ensure that a clone comes before its origin
			assertCloneStatesComesBeforeItsOrigin(t, stateDeps)
			assertCloneComesBeforeItsOrigin(t, datasetDeps)
		})
	}
}

func TestParentSystemState(t *testing.T) {
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
		stateName      string
		linkedStateID  string
		onlyUntagUsers bool

		setPropertyErr bool

		wantErr bool
		isNoOp  bool
	}{
		"Initial state": {}, // This state doesn’t call remove at all but is used to compare golden files

		"Remove user snapshot state":                                    {stateName: "rpool/USERDATA/root_bcde@snaproot1"},
		"Remove user snapshot state (linked to system state: no check)": {stateName: "rpool/USERDATA/root_bcde@snap2"},
		"Remove user snapshot clone state":                              {stateName: "rpool/USERDATA/user4_clone"},

		"Remove system state and only untag user datasets":      {stateName: "rpool/ROOT/ubuntu_8901", onlyUntagUsers: true},
		"Remove user state unconditionnally":                    {stateName: "rpool/USERDATA/user2_2222"},
		"Remove user state conditionnally":                      {stateName: "rpool/USERDATA/user2_2222", linkedStateID: "rpool/ROOT/ubuntu_5678"},
		"Don't remove user state on bad state id match":         {stateName: "rpool/USERDATA/user2_2222", linkedStateID: "doesnt match"},
		"Remove user state unconditionnally linked to 2 states": {stateName: "rpool/USERDATA/user8_gggg-rpool.ROOT.ubuntu-1234"},
		"Unassociate user state linked to one state":            {stateName: "rpool/USERDATA/user8_gggg-rpool.ROOT.ubuntu-1234", linkedStateID: "rpool/ROOT/ubuntu_1234"},

		"Remove system state without user datasets":                                        {stateName: "rpool/ROOT/ubuntu_6789"},
		"Remove system state and its user datasets":                                        {stateName: "rpool/ROOT/ubuntu_8901"},
		"Remove system state remove some user states and unassociate others linked to two": {stateName: "rpool/ROOT/ubuntu_5678"},

		// Error on clones from state. Called with empty linkedStateID
		"Error on removing directly state with state clone":   {stateName: "rpool/USERDATA/user4_for_state_clone", wantErr: true},
		"Error on removing directly state with dataset clone": {stateName: "rpool/USERDATA/user5_for_manual_clone", wantErr: true},

		"Revert unassociate user state if we get an error":            {stateName: "rpool/USERDATA/user2_2222", linkedStateID: "rpool/ROOT/ubuntu_5678", setPropertyErr: true, wantErr: true, isNoOp: true},
		"Don’t destroy system state if user remove issues an error":   {stateName: "rpool/ROOT/ubuntu_5678", setPropertyErr: true, wantErr: true, isNoOp: true},
		"Don’t destroy filesystem state if it has children snapshots": {stateName: "rpool/ROOT/ubuntu_7890", wantErr: true, isNoOp: true},
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

			err = s.remove(context.Background(), &ms, tc.onlyUntagUsers, tc.linkedStateID)
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

func TestSelectStatesToRemove(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		samples       int     // Number of slots in the bucket
		statesToKeep  []int64 // List of seconds from startOfTime
		statesToPlace []int64 // List of seconds from startOfTime
		timeOffset    int64   // Seconds since epoch

		wantStates []string
	}{
		"keep all - bucket has enough capacity": {samples: 5, statesToKeep: []int64{1, 3}, statesToPlace: []int64{2}, wantStates: nil},
		"keep none - bucket is already full":    {samples: 3, statesToKeep: []int64{1, 3, 5}, statesToPlace: []int64{2, 4, 6}, wantStates: []string{"p0", "p1", "p2"}},
		"do not remove keep states":             {samples: 3, statesToKeep: []int64{1, 3, 5, 7}, wantStates: nil},
		"remove one":                            {samples: 4, statesToKeep: []int64{1, 3, 5}, statesToPlace: []int64{2, 4}, wantStates: []string{"p1"}},
		"remove two":                            {samples: 4, statesToKeep: []int64{10, 20, 30}, statesToPlace: []int64{12, 17, 25}, wantStates: []string{"p0", "p2"}},
		"keep oldest":                           {samples: 5, statesToKeep: []int64{20, 25, 30}, statesToPlace: []int64{3, 7, 9, 25}, wantStates: []string{"p2", "p3"}},
		"keep newest":                           {samples: 5, statesToKeep: []int64{0, 5, 10}, statesToPlace: []int64{10, 15, 20, 25}, wantStates: []string{"p0", "p1"}},
		"spread evenly":                         {samples: 7, statesToKeep: []int64{0, 15, 30}, statesToPlace: []int64{0, 5, 10, 15, 25, 30}, wantStates: []string{"p2", "p3"}},
		"spread evenly with offset":             {samples: 7, statesToKeep: []int64{0, 15, 30}, statesToPlace: []int64{0, 5, 10, 15, 25, 30}, timeOffset: 1111111111, wantStates: []string{"p2", "p3"}},

		// no keep states, no states to place
		"no keep state":      {samples: 2, statesToPlace: []int64{1, 2, 4}, wantStates: []string{"p1"}},
		"no states to place": {samples: 2, statesToKeep: []int64{1, 2, 4}, wantStates: nil},

		// same timestamps
		"same timestamp - keep one":  {samples: 4, statesToKeep: []int64{1, 3, 5}, statesToPlace: []int64{2, 2, 4}, wantStates: []string{"p1", "p2"}},
		"same timestamp - keep none": {samples: 3, statesToKeep: []int64{1, 3, 5}, statesToPlace: []int64{2, 2, 4}, wantStates: []string{"p0", "p1", "p2"}},
		"same timestamp - keep all":  {samples: 6, statesToKeep: []int64{1, 3, 5}, statesToPlace: []int64{2, 2, 4}, wantStates: nil},
	}
	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {

			// Initialisation of the states with keep
			var states []stateWithKeep
			for i, s := range tc.statesToKeep {
				lu := time.Unix(tc.timeOffset+s, 0)
				s := State{
					ID:       "k" + strconv.Itoa(i), // Unique index
					LastUsed: lu,
				}
				states = append(states, stateWithKeep{State: &s, keep: keepYes})
			}
			for i, s := range tc.statesToPlace {
				lu := time.Unix(tc.timeOffset+s, 0)
				s := State{
					ID:       "p" + strconv.Itoa(i), // Unique index
					LastUsed: lu,
				}
				states = append(states, stateWithKeep{State: &s, keep: keepUnknown})
			}

			got := selectStatesToRemove(context.Background(), tc.samples, states)
			assertStatesToKeepMatch(t, tc.wantStates, got)
		})
	}
}

func assertStatesToKeepMatch(t *testing.T, want []string, got []*State) {
	var gotIDs []string

	// Extract all the IDs
	for _, g := range got {
		gotIDs = append(gotIDs, g.ID)
	}

	if diff := cmp.Diff(want, gotIDs); diff != "" {
		t.Errorf("states to remove mismatch (-want +got):\n%s", diff)
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

// assertChildrenStatesBeforeParents ensure that all children (snapshots or filesystem states) appears before its parent
func assertChildrenStatesBeforeParents(t *testing.T, deps []*State) {
	t.Helper()

	// iterate on child
	for i, child := range deps {
		parent, snapshot := splitSnapshotName(child.ID)
		if snapshot == "" {
			parent = child.ID[:strings.LastIndex(child.ID, "/")]
		}
		// search corresponding base from the start
		for j, candidate := range deps {
			if candidate.ID != parent {
				continue
			}
			if i > j {
				t.Errorf("Found child %s after its parent %s: %+v", child.ID, candidate.ID, deps)
			}
		}
	}
}

// assertCloneStatesComesBeforeItsOrigin ensure that a clone comes before its origin
func assertCloneStatesComesBeforeItsOrigin(t *testing.T, deps []*State) {
	t.Helper()

	for i, s := range deps {
		for _, datasets := range s.Datasets {
			clone := datasets[0]

			if clone.Origin != "" {
				continue
			}

			// search corresponding origin from the start
			for j, s := range deps {
				for _, datasets := range s.Datasets {
					candidate := datasets[0]

					if candidate.Name != clone.Origin {
						continue
					}
					if i > j {
						t.Errorf("Found clone %s after its origin snapshot %s: %+v", clone.Name, candidate.Name, deps)
					}
				}
			}
		}
	}
}

// assertChildrenBeforeParents ensure that all children (snapshots or filesystem datasets) appears before its parent
func assertChildrenBeforeParents(t *testing.T, deps []*zfs.Dataset) {
	t.Helper()

	// iterate on child
	for i, child := range deps {
		parent, snapshot := splitSnapshotName(child.Name)
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
func assertCloneComesBeforeItsOrigin(t *testing.T, deps []*zfs.Dataset) {
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
			for id, us := range aus {
				if name == id {
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
