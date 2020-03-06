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

			var s *State
		foundState:
			for _, m := range ms.all {
				if tc.stateName == m.ID {
					s = &m.State
					break
				}
				for _, h := range m.History {
					if tc.stateName == h.ID {
						s = h
						break foundState
					}
				}
				for _, aus := range m.AllUsersStates {
					for _, us := range aus {
						if tc.stateName == us.ID {
							s = us
							break foundState
						}
					}
				}
			}

			if s == nil {
				t.Fatalf("No state found matching %s", tc.stateName)
			}

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

// assertDatasetsOrigin compares got maps of origin to reference files, based on test name.
func assertDatasetsOrigin(t *testing.T, got map[string]*string) {
	want := make(map[string]*string)
	testutils.LoadFromGoldenFile(t, got, &want)

	if diff := cmp.Diff(want, got, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("Dataset origin mismatch (-want +got):\n%s", diff)
	}
}
