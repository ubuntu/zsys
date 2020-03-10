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

const timeDay = 24 * int64(time.Hour)
const timeFormat = "2006-01-02 15:04:05"

type sortedReverseByTimeStates []*State

func (s sortedReverseByTimeStates) Len() int { return len(s) }
func (s sortedReverseByTimeStates) Less(i, j int) bool {
	return s[i].LastUsed.After(*s[j].LastUsed)
}
func (s sortedReverseByTimeStates) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

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

// GC starts garbage collection for system and users
func (ms *Machines) GC(ctx context.Context, all bool) error {
	now := time.Now()

	buckets := computeBuckets(now, ms.conf.History)
	keepLast := ms.conf.History.KeepLast

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
			var newestStateIndex int
			var sortedStates sortedReverseByTimeStates
			for _, s := range m.History {
				sortedStates = append(sortedStates, s)
			}
			sort.Sort(sortedStates)

			for _, bucket := range buckets {
				log.Debugf(ctx, i18n.G("bucket %+v"), bucket)

				// End of the array, nothing else to do.
				if newestStateIndex >= len(sortedStates) {
					log.Debug(ctx, i18n.G("No more system states left. Stopping analyzing buckets"))
					break
				}

				log.Debugf(ctx, i18n.G("current state: %s"), sortedStates[newestStateIndex].ID)

				// No states for this bucket, advance to next one.
				if sortedStates[newestStateIndex].LastUsed.Before(bucket.start) {
					log.Debugf(ctx, i18n.G("state.LastUsed (%s) before bucket.start (%s). Continuing"),
						sortedStates[newestStateIndex].LastUsed.Format(timeFormat), bucket.start.Format(timeFormat))
					continue
				}

				// Advance to first state matching this bucket.
				var i int
				for i = newestStateIndex; i < len(sortedStates) && sortedStates[i].LastUsed.After(bucket.start); i++ {
				}
				oldestStateIndex := i - 1
				log.Debugf(ctx, i18n.G("First state matching for this bucket: %s (%s)"), sortedStates[oldestStateIndex].ID, sortedStates[oldestStateIndex].LastUsed.Format(timeFormat))

				// Don't touch anything for this bucket, skip all states in here and advance to next one.
				if bucket.samples == -1 {
					log.Debug(ctx, i18n.G("Keeping all snapshots for this bucket"))
					newestStateIndex = oldestStateIndex + 1
					continue
				}

				// Collect all states for current bucket and mark those having constraints
				states := make([]stateWithKeep, 0, oldestStateIndex-newestStateIndex+1)
				log.Debug(ctx, i18n.G("Collecting all states for current bucket"))
				for i := newestStateIndex; i <= oldestStateIndex; i++ {
					log.Debugf(ctx, i18n.G("Analyzing state %v: %v"), sortedStates[i].ID, sortedStates[i].LastUsed.Format(timeFormat))

					s := sortedStates[i]

					keep := keepUnknown
					if !all && s.isSnapshot() && !strings.Contains(s.ID, "@"+automatedSnapshotPrefix) {
						log.Debugf(ctx, i18n.G("Keeping snapshot %v as it's not a zsys one"), s.ID)
						keep = keepYes
					} else if keep == keepUnknown && i < keepLast {
						log.Debugf(ctx, i18n.G("Keeping snapshot %v as it's in the last %d snapshots"), s.ID, keepLast)
						keep = keepYes
					} else {
						// We only collect systems because users will be untagged if they have any dependency
					analyzeSystemDataset:
						for _, ds := range s.Datasets {
							for _, d := range ds {
								// keep the whole state if any dataset is the origin of a clone of if it’s a clone with snapshots on it
								if byOrigin[d.Name] != nil || snapshotsByDS[d.Name] != nil {
									log.Debugf(ctx, i18n.G("Keeping snapshot %v as at least %s dataset has dependencies"), s.ID, d.Name)
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
					log.Debugf(ctx, i18n.G("No exceeding states for this bucket (delta: %d). Moving on."), nStatesToRemove)
					newestStateIndex = oldestStateIndex + 1
					continue
				}
				log.Debugf(ctx, i18n.G("There are %d exceeding states to potentially remove"), nStatesToRemove)

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
					for _, ds := range s.Datasets {
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
				newestStateIndex = oldestStateIndex + 1
			}
		}

		// Skip unneeded refresh that way
		if !statesChanges {
			break
		}

		// Remove the given states.
		for _, s := range statesToRemove {
			log.Infof(ctx, i18n.G("Removing state: %s"), s.ID)
			if err := s.remove(ctx, ms.z, s.ID, false); err != nil {
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
	var UserStatesToRemove []*State

	for {
		statesChanges := false

		for _, m := range ms.all {
			for _, us := range m.AllUsersStates {
				var newestStateIndex int
				var sortedStates sortedReverseByTimeStates

			nextUserState:
				for _, s := range us {
					// exclude "current" user state fom history
					for _, us := range m.State.Users {
						if us == s {
							continue nextUserState
						}
					}

					sortedStates = append(sortedStates, s)
				}
				sort.Sort(sortedStates)

				for _, bucket := range buckets {
					log.Debugf(ctx, i18n.G("bucket %+v"), bucket)

					// End of the array, nothing else to do.
					if newestStateIndex >= len(sortedStates) {
						log.Debug(ctx, i18n.G("No more user states left. Stopping analyzing buckets"))
						break
					}

					log.Debugf(ctx, i18n.G("current state: %s"), sortedStates[newestStateIndex].ID)

					// No states for this bucket, advance to next one.
					if sortedStates[newestStateIndex].LastUsed.Before(bucket.start) {
						log.Debugf(ctx, i18n.G("state.LastUsed (%s) before bucket.start (%s). Continuing"),
							sortedStates[newestStateIndex].LastUsed.Format(timeFormat), bucket.start.Format(timeFormat))
						continue
					}

					// Advance to first state matching this bucket.
					var i int
					for i = newestStateIndex; i < len(sortedStates) && sortedStates[i].LastUsed.After(bucket.start); i++ {
					}
					oldestStateIndex := i - 1
					log.Debugf(ctx, i18n.G("First state matching for this bucket: %s (%s)"), sortedStates[oldestStateIndex].ID, sortedStates[oldestStateIndex].LastUsed.Format(timeFormat))

					// Don't touch anything for this bucket, skip all states in here and advance to next one.
					if bucket.samples == -1 {
						log.Debug(ctx, i18n.G("Keeping all snapshots for this bucket"))
						newestStateIndex = oldestStateIndex + 1
						continue
					}

					// Collect all states for current bucket and mark those having constraints
					states := make([]stateWithKeep, 0, oldestStateIndex-newestStateIndex+1)
					log.Debug(ctx, i18n.G("Collecting all states for current bucket"))
					for i := newestStateIndex; i <= oldestStateIndex; i++ {
						log.Debugf(ctx, i18n.G("Analyzing state %v: %v"), sortedStates[i].ID, sortedStates[i].LastUsed.Format(timeFormat))
						s := sortedStates[i]

						keep := keepUnknown
						// We can only collect snapshots here for user datasets, or they are unassociated clones that we will clean up later
						if !s.isSnapshot() {
							log.Debugf(ctx, i18n.G("Keeping %v as it's not a snapshot, and necessarily associated to a system state"), s.ID)
							keep = keepYes
						} else if keep == keepUnknown && !all && !strings.Contains(s.ID, "@"+automatedSnapshotPrefix) {
							log.Debugf(ctx, i18n.G("Keeping snapshot %v as it's not a zsys one"), s.ID)
							keep = keepYes
						} else if keep == keepUnknown && i < keepLast {
							log.Debugf(ctx, i18n.G("Keeping snapshot %v as it's in the last %d snapshots"), s.ID, keepLast)
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
									log.Debugf(ctx, i18n.G("Keeping as snapshot %v is associated to a system snapshot"), s.ID)
									keep = keepYes
									break
								}
							}
						} else if keep == keepUnknown {
							// check if any dataset has a automated or manual clone
						analyzeUserDataset:
							for _, ds := range s.Datasets {
								for _, d := range ds {
									// We only treat snapshots as clones are necessarily associated with one system state or
									// has already been destroyed and not associated.
									// do we have clones of us?
									if byOrigin[d.Name] != nil {
										log.Debugf(ctx, i18n.G("Keeping snapshot %v as at least %s dataset has dependencies"), s.ID, d.Name)
										keep = keepYes
										break analyzeUserDataset

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
						log.Debugf(ctx, i18n.G("No exceeding states for this bucket (delta: %d). Moving on."), nStatesToRemove)
						newestStateIndex = oldestStateIndex + 1
						continue
					}
					log.Debugf(ctx, i18n.G("There are %d exceeding states to potentially remove"), nStatesToRemove)

					// FIXME: easy path: Remove first states that we don't keep
					for _, s := range states {
						if s.keep == keepYes {
							continue
						}
						if nStatesToRemove == 0 {
							continue
						}

						// We are removing that state: purge all datasets from our maps.
						for _, ds := range s.Datasets {
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

						UserStatesToRemove = append(UserStatesToRemove, s.State)
						nStatesToRemove--
						statesChanges = true
					}
					newestStateIndex = oldestStateIndex + 1
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
			if err := nt.Destroy(s.Datasets[s.ID][0].Name); err != nil {
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

func (b bucket) String() string {
	return fmt.Sprintf("start: %s end:%s samples: %d", b.start.Format(timeFormat), b.end.Format(timeFormat), b.samples)
}
