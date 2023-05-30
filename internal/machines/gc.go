package machines

import (
	"context"
	"fmt"
	"math"
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
	return s[i].LastUsed.After(s[j].LastUsed)
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
// If all is set manual snapshots are considered too
func (ms *Machines) GC(ctx context.Context, all bool) error {
	now := ms.time.Now()

	buckets := computeBuckets(ctx, now, ms.conf.History)
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
	keepDueToErrorOnDelete := make(map[string]bool)

	// 1. System GC
	var gcPassNum int
	for {
		gcPassNum++
		log.Debugf(ctx, "GC System Pass #%d", gcPassNum)
		statesChanges := false

		for _, m := range ms.all {
			if !m.isZsys() {
				continue
			}

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
					log.Debugf(ctx, "No more system states left for pass #%d.", gcPassNum)
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
				for i = newestStateIndex; i < len(sortedStates) && !sortedStates[i].LastUsed.Before(bucket.start); i++ {
				}
				// The condition became false on i (sortedStates[i].LastUsed is Before bucket.start), this the first one "oldest" matching this bucket is the previous one: i-1
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
					// Previous deletion failed
					if keepDueToErrorOnDelete[s.ID] {
						keep = keepYes
					}
					// In keep last list
					if keep == keepUnknown && i < keepLast {
						log.Debugf(ctx, i18n.G("Keeping snapshot %v as it's in the last %d snapshots"), s.ID, keepLast)
						keep = keepYes
					}
					// Has snapshots as children
					if keep == keepUnknown && !s.isSnapshot() {
						for _, ds := range s.Datasets {
							if ds[0].HasSnapshotInHierarchy() {
								log.Debugf(ctx, i18n.G("Keeping %v as it has a snapshot in its child hierarchy"), s.ID)
								keep = keepYes
							}
						}
					}
					// Non automated snapshots
					if keep == keepUnknown && s.isSnapshot() && !all && !strings.Contains(s.ID, "@"+automatedSnapshotPrefix) {
						log.Debugf(ctx, i18n.G("Keeping snapshot %v as it's not a zsys one"), s.ID)
						keep = keepYes
					}
					// Has clones
					if keep == keepUnknown && s.isSnapshot() {
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
				// next bucket start point
				newestStateIndex = oldestStateIndex + 1

				// Ensure we have the minimum amount of states on this bucket.
				nStatesToRemove := len(states) - bucket.samples
				if nStatesToRemove <= 0 {
					log.Debugf(ctx, i18n.G("No exceeding states for this bucket (delta: %d). Moving on."), nStatesToRemove)
					continue
				}
				log.Debugf(ctx, i18n.G("There are %d exceeding states to potentially remove"), nStatesToRemove)

				statesToRemoveForBucket := selectStatesToRemove(ctx, bucket.samples, states)

				for _, s := range statesToRemoveForBucket {
					statesChanges = true
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
				}

				statesToRemove = append(statesToRemove, statesToRemoveForBucket...)
			}
		}

		// Skip unneeded refresh that way
		if !statesChanges {
			break
		}

		// Remove the given states.
		for _, s := range statesToRemove {
			log.Infof(ctx, i18n.G("Selecting state to remove: %s"), s.ID)
			if err := s.remove(ctx, ms, ""); err != nil {
				log.Errorf(ctx, i18n.G("Couldn't fully destroy state %s: %v\nPutting it in keep list."), s.ID, err)
				keepDueToErrorOnDelete[s.ID] = true
			}
		}
		statesToRemove = nil
		if err := ms.Refresh(ctx); err != nil {
			return fmt.Errorf("Couldn't refresh machine list: %v", err)
		}
		log.Debug(ctx, i18n.G("System have changes, rerun system GC"))
	}

	// 2. GC user datasets. Note that we will only collect user states that are independent of system states.
	// TODO: this is a copy of above, but we keep any states associated with user states, we really need to merge State and UserStates
	// FIXME: user states attached to multiple datasets are counted individually when removing user states, and so, we can think
	// we keep more history than we will have in the end. We should only count them as a single one
	statesToRemove = nil
	// this is the map to preserve unassociated clones because they are filling the bucket policy
	userDatasetsToKeep := make(map[string]bool)
	keepDueToErrorOnDelete = make(map[string]bool)
	gcPassNum = 0
	for {
		gcPassNum++
		log.Debugf(ctx, "GC User Pass #%d", gcPassNum)
		statesChanges := false

		for _, m := range ms.all {
			// FIXME: we count same user state multiple times if linked to multiple bootfs systems
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
						log.Debugf(ctx, "No more user states left for pass #%d.", gcPassNum)
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
					for i = newestStateIndex; i < len(sortedStates) && !sortedStates[i].LastUsed.Before(bucket.start); i++ {
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
						s := sortedStates[i]
						log.Debugf(ctx, i18n.G("Analyzing state %v: %v"), s.ID, s.LastUsed.Format(timeFormat))

						keep := keepUnknown
						// Previous deletion failed
						if keepDueToErrorOnDelete[s.ID] {
							keep = keepYes
						}
						// In keep last list
						if keep == keepUnknown && i < keepLast {
							log.Debugf(ctx, i18n.G("Keeping %v as it's in the last %d snapshots"), s.ID, keepLast)
							keep = keepYes
						}
						// Has snapshots as children
						if keep == keepUnknown && !s.isSnapshot() {
							for _, ds := range s.Datasets {
								if ds[0].HasSnapshotInHierarchy() {
									log.Debugf(ctx, i18n.G("Keeping %v as it has a snapshot in its child hierarchy"), s.ID)
									keep = keepYes
								}
							}
						}
						// Non automated snapshots
						if keep == keepUnknown && s.isSnapshot() && !all && !strings.Contains(s.ID, "@"+automatedSnapshotPrefix) {
							log.Debugf(ctx, i18n.G("Keeping snapshot %v as it's not a zsys one"), s.ID)
							keep = keepYes
						}
						// Filesystem linked to system state
						if keep == keepUnknown && !s.isSnapshot() && s.linkedToSystemState() {
							log.Debugf(ctx, i18n.G("Keeping %v as it's not a snapshot and associated to a system state"), s.ID)
							keep = keepYes
						}
						// Snapshot linked to system state
						if keep == keepUnknown && s.isSnapshot() {
							_, snapshotName := splitSnapshotName(s.ID)
							// Do we have a state associated with us?
							for k := range m.History {
								_, n := splitSnapshotName(k)
								if n == snapshotName {
									log.Debugf(ctx, i18n.G("Keeping as snapshot %v is associated to a system snapshot"), s.ID)
									keep = keepYes
									break
								}
							}
						}

						// Has clones
						if keep == keepUnknown && s.isSnapshot() {
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
						for route := range s.Datasets {
							userDatasetsToKeep[route] = true
						}
					}
					// next bucket start point
					newestStateIndex = oldestStateIndex + 1

					// Ensure we have the minimum amount of states on this bucket.
					nStatesToRemove := len(states) - bucket.samples
					if nStatesToRemove <= 0 {
						log.Debugf(ctx, i18n.G("No exceeding states for this bucket (delta: %d). Moving on."), nStatesToRemove)
						continue
					}
					log.Debugf(ctx, i18n.G("There are %d exceeding states to potentially remove"), nStatesToRemove)

					statesToRemoveForBucket := selectStatesToRemove(ctx, bucket.samples, states)

					for _, s := range statesToRemoveForBucket {
						statesChanges = true
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
					}
					statesToRemove = append(statesToRemove, statesToRemoveForBucket...)
				}
			}
		}
		// Skip uneeded refresh that way
		if !statesChanges {
			break
		}

		// Remove the given states.
		for _, s := range statesToRemove {
			log.Infof(ctx, i18n.G("Selecting state to remove: %s"), s.ID)
			if err := s.remove(ctx, ms, ""); err != nil {
				log.Errorf(ctx, i18n.G("Couldn't fully destroy user state %s: %v.\nPutting it in keep list."), s.ID, err)
				keepDueToErrorOnDelete[s.ID] = true
				continue
			}
			for route := range s.Datasets {
				delete(userDatasetsToKeep, route)
			}
		}

		statesToRemove = nil
		if err := ms.Refresh(ctx); err != nil {
			return fmt.Errorf("Couldn't refresh machine list: %v", err)
		}
		log.Debug(ctx, i18n.G("Users states have changes, rerun user GC"))
	}

	// 3. Clean up unmanaged datasets which were user datasets with empty tags.
	log.Debug(ctx, i18n.G("Unmanaged past user datasets GC"))
	nt := ms.z.NewNoTransaction(ctx)
	keepDatasets := make(map[string]bool)
	keepDueToErrorOnDelete = make(map[string]bool)
	gcPassNum = 0
nextUnmanagedUserPass:
	for {
		log.Debugf(ctx, "GC Unmanaged user Pass #%d", gcPassNum)

		destroyCandidates := make(map[string]*zfs.Dataset)
		for _, d := range ms.unmanagedDatasets {
			// Ignore already treated datasets that we shouldn’t remove
			if _, ok := keepDatasets[d.Name]; ok {
				continue
			}
			// Ignore datasets we can’t destroy
			if _, ok := keepDueToErrorOnDelete[d.Name]; ok {
				continue
			}

			if d.IsSnapshot {
				continue
			}
			r, err := d.IsUserDataset()
			if err != nil {
				log.Warningf(ctx, i18n.G("Couldn't determine if %s was a user dataset %v"), d.Name, err)
				continue
			}
			if !r {
				continue
			}
			destroyCandidates[d.Name] = d
			log.Debugf(ctx, "Adding %s to unmanaged user candidate list", d.Name)
		}

		if len(destroyCandidates) == 0 {
			break
		}

		// pick one random element in the list
		var candidate *zfs.Dataset
		for _, c := range destroyCandidates {
			candidate = c
			break
		}
		log.Debugf(ctx, "Analyzing candidate %s", candidate.Name)
		// Ensure all deps are in removable list
		deps := nt.Dependencies(*candidate)
		for _, d := range deps {
			// Any snapshots has its parent listed in deps or is candidate, ignore it for the check as we didn’t add it to the destroyCandidates list.
			// We only consider filesystem datasets here
			if d.IsSnapshot {
				continue
			}
			if _, ok := destroyCandidates[d.Name]; !ok {
				log.Infof(ctx, "Won’t remove %s: %s is a dependency not listed for auto destruction", candidate.Name, d.Name)
				keepDatasets[candidate.Name] = true
				continue nextUnmanagedUserPass
			}
		}

		log.Debugf(ctx, "Trying to destroy %s", candidate.Name)
		for _, d := range append(deps, candidate) {
			// We destroy here all snapshots and leaf attached. Snapshots won’t be taken into account, however, we don’t want
			// to try destroying leaves again, keep a list.
			if err := nt.Destroy(d.Name); err != nil {
				log.Warningf(ctx, i18n.G("Couldn't destroy user dataset %s (due to %s): %v"), d.Name, candidate.Name, err)
				keepDueToErrorOnDelete[d.Name] = true
			}
		}

		if err := ms.Refresh(ctx); err != nil {
			return fmt.Errorf("Couldn't refresh machine list: %v", err)
		}
		gcPassNum++
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
func computeBuckets(ctx context.Context, now time.Time, rules config.HistoryRules) (buckets []bucket) {
	log.Debugf(ctx, "calculating buckets")
	nowDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	end := nowDay.Add(time.Duration(-rules.GCStartAfter * timeDay))

	// most recent bucket where we keep all snapshots
	buckets = append(buckets, bucket{
		start:   end,
		end:     now,
		samples: -1,
	})
	log.Debugf(ctx, "bucket keep all: start:%s, end:%s, samples:%d", buckets[len(buckets)-1].start, buckets[len(buckets)-1].end, buckets[len(buckets)-1].samples)

	for _, rule := range rules.GCRules {
		log.Debugf(ctx, "Rule %s, buckets: %d, length, %d", rule.Name, rule.Buckets, rule.BucketLength)
		buckerDuration := time.Duration(timeDay * rule.BucketLength)

		startPeriod := end.Add(-time.Duration(int(buckerDuration) * rule.Buckets))

		for d := end.Add(-buckerDuration); d.After(startPeriod) || d == startPeriod; d = d.Add(-buckerDuration) {
			buckets = append(buckets, bucket{
				start:   d,
				end:     d.Add(buckerDuration),
				samples: rule.SamplesPerBucket,
			})
			log.Debugf(ctx, "  -  start:%s end:%s samples:%d", buckets[len(buckets)-1].start, buckets[len(buckets)-1].end, buckets[len(buckets)-1].samples)
		}
		end = startPeriod
	}

	// Oldest bucket cannot hold any datasets
	buckets = append(buckets, bucket{
		start:   time.Time{},
		end:     end,
		samples: 0,
	})
	log.Debugf(ctx, "bucket oldest: start:%s end:%s samples:%d", buckets[len(buckets)-1].start, buckets[len(buckets)-1].end, buckets[len(buckets)-1].samples)

	return buckets
}

func (b bucket) String() string {
	return fmt.Sprintf("start: %s end:%s samples: %d", b.start.Format(timeFormat), b.end.Format(timeFormat), b.samples)
}

// selectStatesToRemove selects the maximum number of states to keep to fill a bucket up to samples and spread them evenly over the width of the bucket.
// When 2 solutions are equal the first match is kept
func selectStatesToRemove(ctx context.Context, samples int, states []stateWithKeep) (statesToRemove []*State) {
	log.Debug(ctx, "selecting list of states to remove")

	if len(states) <= samples {
		log.Debug(ctx, "bucket has enough capacity to keep all the states")
		return statesToRemove
	}

	var sumKeep int64 // Start is oldest time and end is newest
	var toPlace []stateWithKeep
	var toKeep []stateWithKeep
	var end, start float64

	// States are supposed to be sorted by reverse time but we cannot assume it's true in this scope
	for i, e := range states {
		t := float64(e.LastUsed.Unix())
		if i == 0 {
			end = t
			start = t
		}
		if t < start {
			start = t
		}
		if t > end {
			end = t
		}
	}
	log.Debugf(ctx, "Interval: %.2f - %.2f", start, end)
	for _, s := range states {
		if s.keep == keepYes {
			sumKeep += s.LastUsed.Unix()
			toKeep = append(toKeep, s)
			continue
		}
		toPlace = append(toPlace, s)
	}

	freeSlots := samples - len(toKeep)
	if freeSlots <= 0 {
		// The bucket is full, do not keep anything
		log.Debug(ctx, "bucket is full, removing all non-keep states")
		for _, s := range toPlace {
			statesToRemove = append(statesToRemove, s.State)
		}
		return statesToRemove
	}
	cs := combinations(len(toPlace), freeSlots)

	var bestCombination []int
	var bestIndex int
	minDistance := end - start
	log.Debugf(ctx, "Existing n: %d, minDist: %.3f, barycenter: %.3f", len(toKeep), minDistance, start+minDistance/2)

	var dbgMsg string
	for i, c := range cs {
		dbgMsg = fmt.Sprintf("    %d - ", i)

		var sumToPlace int64
		for i := 0; i < freeSlots; i++ {
			sumToPlace += toPlace[c[i]].LastUsed.Unix()
			dbgMsg += fmt.Sprintf("%d:%d ", c[i], toPlace[c[i]].LastUsed.Unix())
		}

		avg := float64(sumKeep+sumToPlace) / float64(samples)
		curDistance := math.Abs(avg - (start + (end-start)/2))
		if curDistance < minDistance {
			minDistance = curDistance
			bestCombination = c
			bestIndex = i
		}
		log.Debugf(ctx, "%s%d %.3f %.3f", dbgMsg, sumToPlace, avg, curDistance)
	}

	dbgMsg = fmt.Sprintf("Best solution: dist=%.3f toPlace[%d] = [ ", minDistance, bestIndex)
	for _, c := range bestCombination {
		dbgMsg += fmt.Sprintf("%s:%d ", toPlace[c].ID, toPlace[c].LastUsed.Unix())
	}
	log.Debugf(ctx, "%s]", dbgMsg)

	// Keep bestCombination and remove everything else not marked toKeep
	for _, c := range bestCombination {
		toPlace[c].keep = keepYes
	}

	for _, s := range toPlace {
		if s.keep == keepYes {
			continue
		}
		statesToRemove = append(statesToRemove, s.State)
	}
	return statesToRemove
}

// linkedToSystemState returns if a datasets is potentially linked to a system state.
// Note that it doesn’t check if the system state is currently accessible.
func (s *State) linkedToSystemState() bool {
	for _, ds := range s.Datasets {
		if ds[0].BootfsDatasets != "" {
			return true
		}
	}
	return false
}

const (
	badNegInput = "combin: negative input"
	badSetSize  = "combin: n < k"
)

// combinations generates all of the combinations of k elements from a
// set of size n. The returned slice has length Binomial(n,k) and each inner slice
// has length k.
//
// n and k must be non-negative with n >= k, otherwise Combinations will panic.
//
// This is copied from gonumonum
func combinations(n, k int) [][]int {
	combinations := binomial(n, k)
	data := make([][]int, combinations)
	if len(data) == 0 {
		return data
	}
	data[0] = make([]int, k)
	for i := range data[0] {
		data[0][i] = i
	}
	for i := 1; i < combinations; i++ {
		next := make([]int, k)
		copy(next, data[i-1])
		nextCombination(next, n, k)
		data[i] = next
	}
	return data
}

// binomial returns the binomial coefficient of (n,k), also commonly referred to
// as "n choose k".
//
// The binomial coefficient, C(n,k), is the number of unordered combinations of
// k elements in a set that is n elements big, and is defined as
//
//	C(n,k) = n!/((n-k)!k!)
//
// n and k must be non-negative with n >= k, otherwise Binomial will panic.
// No check is made for overflow.
// This is copied from gonum
func binomial(n, k int) int {
	if n < 0 || k < 0 {
		panic(badNegInput)
	}
	if n < k {
		panic(badSetSize)
	}
	// (n,k) = (n, n-k)
	if k > n/2 {
		k = n - k
	}
	b := 1
	for i := 1; i <= k; i++ {
		b = (n - k + i) * b / i
	}
	return b
}

// nextCombination generates the combination after s, overwriting the input value.
// This is copied from gonum
func nextCombination(s []int, n, k int) {
	for j := k - 1; j >= 0; j-- {
		if s[j] == n+j-k {
			continue
		}
		s[j]++
		for l := j + 1; l < k; l++ {
			s[l] = s[j] + l - j
		}
		break
	}
}
