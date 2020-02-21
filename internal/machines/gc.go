package machines

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/log"
	"github.com/ubuntu/zsys/internal/zfs"
)

type bucket struct {
	start   time.Time
	end     time.Time
	samples int // Number of datasets to keep / bucket
}

const timeDay = 24 * int(time.Hour)

type sortedReverseByTimeStates []*State

func (s sortedReverseByTimeStates) Len() int { return len(s) }
func (s sortedReverseByTimeStates) Less(i, j int) bool {
	return s[i].LastUsed.After(*s[j].LastUsed)
}
func (s sortedReverseByTimeStates) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

type sortedReverseByTimeUserStates []UserState

func (s sortedReverseByTimeUserStates) Len() int { return len(s) }
func (s sortedReverseByTimeUserStates) Less(i, j int) bool {
	return s[i].LastUsed.After(*s[j].LastUsed)
}
func (s sortedReverseByTimeUserStates) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

type keepStatus int

const (
	keepUnknown keepStatus = iota
	keepYes     keepStatus = iota
	keepNo      keepStatus = iota
)

type stateWithKeep struct {
	*State
	keep keepStatus
}

type userStateWithKeep struct {
	*UserState
	keep keepStatus
}

// GC starts garbage collection for system and users
func (ms *Machines) GC(ctx context.Context) error {
	now := time.Now()

	buckets := computeBuckets(now, ms.conf.History)

	allDatasets := make([]*zfs.Dataset, 0, len(ms.allSystemDatasets)+len(ms.allPersistentDatasets)+len(ms.allUsersDatasets)+len(ms.unmanagedDatasets))

	byOrigin := make(map[string][]string)      // list of clones for a given origin (snapshot)
	snapshotsByDS := make(map[string][]string) // List of snapshots for a given dataset

	log.Debug(ctx, i18n.G("Collect datasets"))
	allDatasets = append(allDatasets, ms.allSystemDatasets...)
	allDatasets = append(allDatasets, ms.allPersistentDatasets...)
	allDatasets = append(allDatasets, ms.allUsersDatasets...)
	allDatasets = append(allDatasets, ms.unmanagedDatasets...)

	for _, d := range allDatasets {
		if !d.IsSnapshot && d.Origin != "" {
			byOrigin[d.Origin] = append(byOrigin[d.Origin], d.Name)
		} else if d.IsSnapshot {
			n, _ := splitSnapshotName(d.Name)
			snapshotsByDS[n] = append(snapshotsByDS[n], d.Name)
		}
	}

	var statesToRemove []*State

	// 1. System GC
	log.Debug(ctx, i18n.G("System GC"))

	for {
		statesChanges := false

		for _, m := range ms.all {
			var lastStateIndex int
			var sortedStates sortedReverseByTimeStates
			for _, s := range m.History {
				sortedStates = append(sortedStates, s)
			}
			sort.Sort(sortedStates)

			for _, bucket := range buckets {
				// End of the array, nothing else to do.
				if lastStateIndex > len(sortedStates) {
					break
				}

				// No states for this bucket, advance to next one.
				if sortedStates[lastStateIndex].LastUsed.After(bucket.start) {
					continue
				}

				// Don't touch anything for this bucket, skip all states in here and advance to next one.
				if bucket.samples == -1 {
					var i int
					for i = lastStateIndex; sortedStates[i].LastUsed.After(bucket.start); i++ {
					}
					lastStateIndex = i
					continue
				}

				// Advance to first state matching this bucket.
				var i int
				for i = lastStateIndex; sortedStates[i].LastUsed.After(bucket.start); i++ {
				}
				firstStateIndex := i - 1

				// Collect all states for current bucket and mark those having constraints
				states := make([]stateWithKeep, 0, lastStateIndex-firstStateIndex+1)
				for i := lastStateIndex; i < firstStateIndex; i++ {
					s := sortedStates[i]

					keep := keepUnknown
					if s.isSnapshot() && !strings.Contains(s.ID, "@"+automatedSnapshotPrefix) {
						keep = keepYes
					} else {
						// We only collect systems because users will be untagged if they have any dependency
					analyzeSystemDataset:
						for _, ds := range s.SystemDatasets {
							for _, d := range ds {
								// keep the whole state if any dataset is the origin of a clone of if it’s a clone with snapshots on it
								if byOrigin[d.Name] != nil || snapshotsByDS[d.Name] != nil {
									keep = keepYes
									break analyzeSystemDataset
								}
							}
						}
					}

					states = append(states, stateWithKeep{
						State: s,
						keep:  keep,
					})
				}

				// Ensure we have the minimum amount of states on this bucket.
				nStatesToRemove := len(states) - bucket.samples
				if nStatesToRemove <= 0 {
					continue
				}

				// FIXME: easy path: Remove first states that we don't keep
				for _, s := range states {
					if s.keep == keepYes {
						continue
					}
					if nStatesToRemove == 0 {
						continue
					}

					// We are removing that state: purge all datasets from our maps.
					// We don’t deal with user datasets right now as we only untag them.
					for _, ds := range s.SystemDatasets {
						for _, d := range ds {
							if d.IsSnapshot {
								n, _ := splitSnapshotName(d.Name)
								snapshotsByDS[n] = removeFromSlice(snapshotsByDS[n], d.Name)
							} else {
								for orig := range byOrigin {
									byOrigin[orig] = removeFromSlice(byOrigin[orig], d.Name)
								}
							}
						}
					}

					statesToRemove = append(statesToRemove, s.State)
					nStatesToRemove--
					statesChanges = true
				}
				lastStateIndex = firstStateIndex + 1
			}
		}

		// Skip uneeded refresh that way
		if !statesChanges {
			break
		}

		// Remove the given states.
		for _, s := range statesToRemove {
			log.Infof(ctx, i18n.G("Removing state: %s"), s.ID)
			if err := s.Remove(ctx, ms.z); err != nil {
				log.Errorf(ctx, i18n.G("Couldn't fully destroy state %s: %v"), s.ID, err)
			}
		}
		statesToRemove = nil
		if err := ms.Refresh(ctx); err != nil {
			return fmt.Errorf("Couldn't refresh machine list: %v", err)
		}
		log.Debug(ctx, i18n.G("System have changes, rerun system GC"))
	}

	// 2. GC user datasets
	log.Debug(ctx, i18n.G("User GC"))
	// TODO: this is a copy of above, but we keep any states associated with user states, we really need to merge State and UserStates
	var UserStatesToRemove []*UserState

	for {
		statesChanges := false

		for _, m := range ms.all {
			var lastStateIndex int
			for _, us := range m.Users {
				var sortedStates sortedReverseByTimeUserStates

				for _, s := range us {
					sortedStates = append(sortedStates, s)
				}
				sort.Sort(sortedStates)

				for _, bucket := range buckets {
					// End of the array, nothing else to do.
					if lastStateIndex > len(sortedStates) {
						break
					}

					// No states for this bucket, advance to next one.
					if sortedStates[lastStateIndex].LastUsed.After(bucket.start) {
						continue
					}

					// Don't touch anything for this bucket, skip all states in here and advance to next one.
					if bucket.samples == -1 {
						var i int
						for i = lastStateIndex; sortedStates[i].LastUsed.After(bucket.start); i++ {
						}
						lastStateIndex = i
						continue
					}

					// Advance to first state matching this bucket.
					var i int
					for i = lastStateIndex; sortedStates[i].LastUsed.After(bucket.start); i++ {
					}
					firstStateIndex := i - 1

					// Collect all states for current bucket and mark those having constraints
					states := make([]userStateWithKeep, 0, lastStateIndex-firstStateIndex+1)
					for i := lastStateIndex; i < firstStateIndex; i++ {
						s := sortedStates[i]

						keep := keepUnknown
						// We can only collect snapshots here for user datasets, or they are unassociated clones that we will clean up later
						if !s.isSnapshot() {
							keep = keepYes
						} else if keep == keepUnknown {
							_, snapshotName := splitSnapshotName(s.ID)
							// Do we have a state associated with us?
							for k, state := range m.History {
								// TODO: if we associate a real userState to a State, we can compare them directly
								if !state.isSnapshot() {
									continue
								}
								_, n := splitSnapshotName(k)
								if n == snapshotName {
									keep = keepYes
									break
								}
							}
						} else if keep == keepUnknown {
							// check if any dataset has a automated or manual clone
						analyzeUserDataset:
							for _, d := range s.Datasets {
								// We only treat snapshots as clones are necessarily associated with one system state or
								// has already been destroyed and not associated.
								// do we have clones of us?
								if byOrigin[d.Name] != nil {
									keep = keepYes
									break analyzeUserDataset

								}
							}
						}

						states = append(states, userStateWithKeep{
							UserState: &s,
							keep:      keep,
						})
					}

					// Ensure we have the minimum amount of states on this bucket.
					nStatesToRemove := len(states) - bucket.samples
					if nStatesToRemove <= 0 {
						continue
					}

					// FIXME: easy path: Remove first states that we don't keep
					for _, s := range states {
						if s.keep == keepYes {
							continue
						}
						if nStatesToRemove == 0 {
							continue
						}

						// We are removing that state: purge all datasets from our maps.
						for _, d := range s.Datasets {
							if d.IsSnapshot {
								n, _ := splitSnapshotName(d.Name)
								snapshotsByDS[n] = removeFromSlice(snapshotsByDS[n], d.Name)
							} else {
								for orig := range byOrigin {
									byOrigin[orig] = removeFromSlice(byOrigin[orig], d.Name)
								}
							}
						}

						UserStatesToRemove = append(UserStatesToRemove, s.UserState)
						nStatesToRemove--
						statesChanges = true
					}
					lastStateIndex = firstStateIndex + 1
				}
			}
		}
		// Skip uneeded refresh that way
		if !statesChanges {
			break
		}

		// Remove the given states.
		nt := ms.z.NewNoTransaction(ctx)
		for _, s := range UserStatesToRemove {
			log.Infof(ctx, i18n.G("Removing state: %s"), s.ID)
			if err := nt.Destroy(s.Datasets[0].Name); err != nil {
				log.Errorf(ctx, i18n.G("Couldn't destroy user state %s: %v"), s, err)
			}
		}

		UserStatesToRemove = nil
		// TODO: only if changes
		if err := ms.Refresh(ctx); err != nil {
			return fmt.Errorf("Couldn't refresh machine list: %v", err)
		}
		log.Debug(ctx, i18n.G("Users states have changes, rerun user GC"))
	}

	// 3. Clean up user datasets with no tags. Take into account user datasets with a child not associated with anything but parent is
	// (they, and all snapshots on them will end up in unmanaged datasets)
	log.Debug(ctx, i18n.G("Unassociated user datasets GC"))
	var alreadyDestroyedRoot []string
	nt := ms.z.NewNoTransaction(ctx)
nextDataset:
	for _, d := range ms.unmanagedDatasets {
		if d.IsSnapshot || !isUserDataset(d.Name) {
			continue
		}
		if d.BootfsDatasets != "" {
			continue
		}
		for _, n := range alreadyDestroyedRoot {
			if strings.HasPrefix(d.Name, n+"/") {
				continue nextDataset
			}
		}

		// We destroy here all snapshots and leaf attached. Snapshots won’t be taken into account, however, we don’t want
		// to try destroying leaves again, keep a list.
		if err := nt.Destroy(d.Name); err != nil {
			log.Warningf(ctx, i18n.G("Couldn't destroy unmanaged user dataset %s: %v"), d.Name, err)
		}

		alreadyDestroyedRoot = append(alreadyDestroyedRoot, d.Name)
	}

	return nil
}

func removeFromSlice(s []string, name string) (r []string) {
	var i int
	var v string
	var found bool
	for i, v = range s {
		if v == name {
			found = true
			break
		}
	}
	if !found {
		return s
	}

	s[i] = s[len(s)-1]
	// We do not need to put s[i] at the end, as it will be discarded anyway
	r = s[:len(s)-1]
	if len(r) == 0 {
		r = nil
	}
	return r
}

// computeBuckets initializes the list of buckets in which the dataset will be sorted.
// Buckets are defined from the main configuration file.
func computeBuckets(now time.Time, rules config.HistoryRules) (buckets []bucket) {
	nowDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	end := nowDay.Add(time.Duration(-rules.GCStartAfter * timeDay))

	// most recent bucket where we keep all snapshots
	buckets = append(buckets, bucket{
		start:   end,
		end:     now,
		samples: -1,
	})

	for _, rule := range rules.GCRules {
		buckerDuration := time.Duration(timeDay * rule.BucketLength)

		startPeriod := end.Add(-time.Duration(int(buckerDuration) * rule.Buckets))

		for d := end.Add(-buckerDuration); d.After(startPeriod) || d == startPeriod; d = d.Add(-buckerDuration) {
			buckets = append(buckets, bucket{
				start:   d,
				end:     d.Add(buckerDuration),
				samples: rule.SamplesPerBucket,
			})
		}
		end = startPeriod
	}

	// Oldest bucket cannot hold any datasets
	buckets = append(buckets, bucket{
		start:   time.Time{},
		end:     end,
		samples: 0,
	})

	return buckets
}

// validate checks that we are still in a valid state for this bucket,
// or that we didn’t degrade from an already invalid state
func (b bucket) validate(oldState, newState []*stateWithKeep) bool {
	if b.samples == -1 {
		return len(oldState) == len(newState)
	}

	newDistance := len(newState) - b.samples
	if newDistance >= 0 {
		return true
	}

	oldDistance := len(oldState) - b.samples

	// We degraded even more an already degraded state, return false.
	if newDistance < oldDistance {
		return false
	}
	return true
}
