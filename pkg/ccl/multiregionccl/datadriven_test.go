// Copyright 2021 The Cockroach Authors.
//
// Licensed as a CockroachDB Enterprise file under the Cockroach Community
// License (the "License"); you may not use this file except in compliance with
// the License. You may obtain a copy of the License at
//
//     https://github.com/cockroachdb/cockroach/blob/master/licenses/CCL.txt

package multiregionccl_test

import (
	"context"
	gosql "database/sql"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/cockroach/pkg/base"
	"github.com/cockroachdb/cockroach/pkg/keys"
	"github.com/cockroachdb/cockroach/pkg/kv/kvbase"
	"github.com/cockroachdb/cockroach/pkg/kv/kvclient/kvcoord"
	"github.com/cockroachdb/cockroach/pkg/kv/kvserver"
	"github.com/cockroachdb/cockroach/pkg/roachpb"
	"github.com/cockroachdb/cockroach/pkg/sql"
	"github.com/cockroachdb/cockroach/pkg/testutils"
	"github.com/cockroachdb/cockroach/pkg/testutils/serverutils"
	"github.com/cockroachdb/cockroach/pkg/testutils/skip"
	"github.com/cockroachdb/cockroach/pkg/testutils/testcluster"
	"github.com/cockroachdb/cockroach/pkg/util/leaktest"
	"github.com/cockroachdb/cockroach/pkg/util/log"
	"github.com/cockroachdb/cockroach/pkg/util/syncutil"
	"github.com/cockroachdb/cockroach/pkg/util/tracing"
	"github.com/cockroachdb/datadriven"
	"github.com/cockroachdb/errors"
)

// TestMultiRegionDataDriven is a data-driven test to test various multi-region
// invariants at a high level. This is accomplished by allowing custom cluster
// configurations when creating the test cluster and providing directives to
// assert expectations in query traces.
//
// It offers the following commands:
//
// "new-cluster localities=<localities>": creates a new cluster with
// len(localities) number of nodes. A locality entry may be repeated to
// create more than one node in a region. The order in which the localities are
// provided may later be used to index into a particular node to run a query.
//
// "exec-sql idx=server_number": executes the input SQL query on the target
// server.
//
// "trace-sql idx=server_number":
// Similar to exec-sql, but also traces the input statement and analyzes the
// trace. Currently, the trace analysis only works for "simple" queries which
// perform a single kv operation. The trace is analyzed for the following:
// 	 - served locally: prints true iff the query was routed to the local
//   replica.
//   - served via follower read: prints true iff the query was served using a
//   follower read. This is omitted completely if the query was not served
//   locally.
// This is because it the replica the query is routed to may or may not be the
// leaseholder.
//
// "wait-for-zone-config-changes idx=server_number table-name=tbName
// [num-voters=num] [num-non-voters=num] [leaseholder=idx] [voter=idx_list]
// [non-voter=idx_list] [not-present=idx_list]": finds the
// range belonging to the given tbName's prefix key and runs it through the
// split, replicate, and raftsnapshot queues. If the num-voters or
// num-non-voters arguments are provided, it then makes sure that the range
// conforms to those.
// It also takes the arguments leaseholder, voter, nonvoter, and not-present,
// each of which take a comma-separated list of server indexes to check against
// the provided table.
// Note that if a server idx is not listed, it is unconstrained. This is
// necessary for cases where we know how many replicas of a type we want but
// we can't deterministically know their placement.
// If the leaseholder does not match the argument provided, a lease
// transfer is attempted.
// All this is done in a succeeds soon as any of these steps may error out for
// completely legitimate reasons.
//
// NB: It's worth noting that num-voters and num-non-voters must be provided as
// arguments and cannot be asserted on return similar to the way a lot of
// data-driven tests would do. This is because zone config updates, which are
// gossiped, may not have been seen by the leaseholder node when the replica is
// run through the replicate queue. As such, it is completely reasonable to have
// this operation retry until the node has seen these updates, which is why this
// whole operation (including asserting num-voters num-non-voters) is wrapped
// inside a succeeds soon. The only way to do this is to accept these things as
// arguments, instead of returning the result.
//
// "refresh-range-descriptor-cache idx=server_number table-name=tbName": runs
// the given query to refresh the range descriptor cache. Then, using the
// table-name argument, the closed timestamp policy is returned.
//
// "sleep-for-follower-read": sleeps for 4,4 seconds, the target duration for
// follower reads according to our cluster settings.
//
// "cleanup-cluster": destroys the cluster. Must be done before creating a new
// cluster.
func TestMultiRegionDataDriven(t *testing.T) {
	defer leaktest.AfterTest(t)()
	defer log.Scope(t).Close(t)

	skip.UnderRace(t, "flaky test")

	ctx := context.Background()
	datadriven.Walk(t, "testdata/", func(t *testing.T, path string) {
		ds := datadrivenTestState{}
		defer ds.cleanup(ctx)
		var mu syncutil.Mutex
		var traceStmt string
		var recCh chan tracing.Recording
		datadriven.RunTest(t, path, func(t *testing.T, d *datadriven.TestData) string {
			switch d.Cmd {
			case "sleep-for-follower-read":
				time.Sleep(time.Millisecond * 4400)
			case "new-cluster":
				if ds.tc != nil {
					t.Fatal("cluster already exists, cleanup cluster first")
				}
				var localities string
				mustHaveArgOrFatal(t, d, serverLocalities)
				d.ScanArgs(t, serverLocalities, &localities)
				if localities == "" {
					t.Fatalf("can't create a cluster without %s", serverLocalities)
				}
				serverArgs := make(map[int]base.TestServerArgs)
				localityNames := strings.Split(localities, ",")
				recCh = make(chan tracing.Recording, 1)
				for i, localityName := range localityNames {
					localityCfg := roachpb.Locality{
						Tiers: []roachpb.Tier{
							{Key: "region", Value: localityName},
							{Key: "zone", Value: localityName + "a"},
						},
					}
					serverArgs[i] = base.TestServerArgs{
						Locality: localityCfg,
						Knobs: base.TestingKnobs{
							SQLExecutor: &sql.ExecutorTestingKnobs{
								WithStatementTrace: func(trace tracing.Recording, stmt string) {
									mu.Lock()
									defer mu.Unlock()
									if stmt == traceStmt {
										recCh <- trace
									}
								},
							},
						},
					}
				}
				numServers := len(localityNames)
				tc := testcluster.StartTestCluster(t, numServers, base.TestClusterArgs{
					ServerArgsPerNode: serverArgs,
				})
				ds.tc = tc

				sqlConn, err := ds.getSQLConn(0)
				if err != nil {
					return err.Error()
				}
				// Speed up closing of timestamps, as we need in order to be able to use
				// follower_read_timestamp().
				// Every 0.2s we'll close the timestamp from 0.4s ago. We'll attempt
				// follower reads for anything below 0.4s * (1 + 0.5 * 20) = 4.4s.
				_, err = sqlConn.Exec(
					"SET CLUSTER SETTING kv.closed_timestamp.target_duration = '0.4s'")
				if err != nil {
					return err.Error()
				}
				_, err = sqlConn.Exec("SET CLUSTER SETTING kv.closed_timestamp.close_fraction = 0.5")
				if err != nil {
					return err.Error()
				}
				_, err = sqlConn.Exec("SET CLUSTER SETTING kv.follower_read.target_multiple = 20")
				if err != nil {
					return err.Error()
				}

			case "cleanup-cluster":
				ds.cleanup(ctx)

			case "exec-sql":
				mustHaveArgOrFatal(t, d, serverIdx)
				var err error
				var idx int
				d.ScanArgs(t, serverIdx, &idx)
				sqlDB, err := ds.getSQLConn(idx)
				if err != nil {
					return err.Error()
				}

				_, err = sqlDB.Exec(d.Input)
				if err != nil {
					return err.Error()
				}

			case "trace-sql":
				mustHaveArgOrFatal(t, d, serverIdx)
				queryFunc := func() (localRead bool, followerRead bool, err error) {
					var idx int
					d.ScanArgs(t, serverIdx, &idx)
					sqlDB, err := ds.getSQLConn(idx)
					if err != nil {
						return false, false, err
					}

					// Setup tracing for the input.
					mu.Lock()
					traceStmt = d.Input
					mu.Unlock()
					defer func() {
						mu.Lock()
						traceStmt = ""
						mu.Unlock()
					}()

					_, err = sqlDB.Query(d.Input)
					if err != nil {
						return false, false, err
					}
					rec := <-recCh
					localRead, followerRead, err = checkReadServedLocallyInSimpleRecording(rec)
					if err != nil {
						return false, false, err
					}
					return localRead, followerRead, nil
				}
				localRead, followerRead, err := queryFunc()
				if err != nil {
					return err.Error()
				}
				var output strings.Builder
				output.WriteString(
					fmt.Sprintf("served locally: %s\n", strconv.FormatBool(localRead)))
				// Only print follower read information if the query was served locally.
				if localRead {
					output.WriteString(
						fmt.Sprintf("served via follower read: %s\n", strconv.FormatBool(followerRead)))
				}
				return output.String()

			case "refresh-range-descriptor-cache":
				mustHaveArgOrFatal(t, d, tableName)
				mustHaveArgOrFatal(t, d, serverIdx)

				var tbName string
				d.ScanArgs(t, tableName, &tbName)
				var idx int
				d.ScanArgs(t, serverIdx, &idx)
				sqlDB, err := ds.getSQLConn(idx)
				if err != nil {
					return err.Error()
				}
				// Execute the query that's supposed to populate the range descriptor
				// cache.
				_, err = sqlDB.Exec(d.Input)
				if err != nil {
					return err.Error()
				}
				// Ensure the range descriptor cache was indeed populated.
				var tableID uint32
				err = sqlDB.QueryRow(`SELECT id from system.namespace WHERE name=$1`,
					tbName).Scan(&tableID)
				if err != nil {
					return err.Error()
				}
				cache := ds.tc.Server(idx).DistSenderI().(*kvcoord.DistSender).RangeDescriptorCache()
				tablePrefix := keys.MustAddr(keys.SystemSQLCodec.TablePrefix(tableID))
				entry := cache.GetCached(ctx, tablePrefix, false /* inverted */)
				if entry == nil {
					return errors.Newf("no entry found for %s in cache", tbName).Error()
				}
				return entry.ClosedTimestampPolicy().String()

			case "wait-for-zone-config-changes":
				mustHaveArgOrFatal(t, d, tableName)
				mustHaveArgOrFatal(t, d, serverIdx)

				var idx int
				d.ScanArgs(t, serverIdx, &idx)
				sqlDB, err := ds.getSQLConn(idx)
				if err != nil {
					return err.Error()
				}
				var tbName string
				d.ScanArgs(t, tableName, &tbName)
				var tableID uint32
				err = sqlDB.QueryRow(
					`SELECT id from system.namespace WHERE name=$1`, tbName).Scan(&tableID)
				if err != nil {
					return err.Error()
				}
				tablePrefix := keys.MustAddr(keys.SystemSQLCodec.TablePrefix(tableID))

				// There's a lot going on here and things can fail at various steps, for
				// completely legitimate reasons, which is why this thing needs to be
				// wrapped in a succeeds soon.
				if err := testutils.SucceedsSoonError(func() error {
					desc, err := ds.tc.LookupRange(tablePrefix.AsRawKey())
					if err != nil {
						return err
					}

					// Parse user-supplied expected replica types from input.
					expectedPlacement := parseReplicasFromInput(t, ds.tc, d)
					// Get current replica type info from range and validate against
					// user-specified replica types
					actualPlacement, err := parseReplicasFromRange(t, ds.tc, desc)
					if err != nil {
						return err
					}

					leaseHolderNode := ds.tc.Server(actualPlacement.getLeaseholder())
					store, err := leaseHolderNode.GetStores().(*kvserver.Stores).GetStore(
						leaseHolderNode.GetFirstStoreID())
					if err != nil {
						return err
					}
					repl := store.LookupReplica(tablePrefix)
					if repl == nil {
						return errors.New(`could not find replica`)
					}
					for _, queueName := range []string{"split", "replicate", "raftsnapshot"} {
						_, processErr, err := store.ManuallyEnqueue(ctx, queueName, repl,
							true /* skipShouldQueue */)
						if processErr != nil {
							return processErr
						}
						if err != nil {
							return err
						}
					}

					// If the user specified a leaseholder, transfer range lease to the
					// leaseholder.
					if expectedPlacement.hasLeaseholderInfo() {
						expectedLeaseIdx := expectedPlacement.getLeaseholder()
						actualLeaseIdx := actualPlacement.getLeaseholder()
						if expectedLeaseIdx != actualLeaseIdx {
							leaseErr := errors.Newf("expected leaseholder %d but got %d",
								expectedLeaseIdx, actualLeaseIdx)

							// We want to only initiate a lease transfer if our target replica
							// is a voter. Otherwise, TransferRangeLease will silently fail.
							newLeaseholderType := actualPlacement.getReplicaType(expectedLeaseIdx)
							if newLeaseholderType != replicaTypeVoter {
								return errors.CombineErrors(
									leaseErr,
									errors.Newf(
										"expected node %s to be a voter but was %s",
										expectedLeaseIdx,
										newLeaseholderType.String()))
							}

							log.VEventf(
								ctx,
								2,
								"transferring lease from node %d to %d", actualLeaseIdx, expectedLeaseIdx)
							err = ds.tc.TransferRangeLease(desc, ds.tc.Target(expectedLeaseIdx))
							return errors.CombineErrors(
								leaseErr,
								err,
							)
						}
					}

					err = actualPlacement.satisfiesExpectedPlacement(expectedPlacement)
					if err != nil {
						return err
					}

					if d.HasArg(numVoters) {
						var voters int
						d.ScanArgs(t, numVoters, &voters)
						if len(desc.Replicas().VoterDescriptors()) != voters {
							return errors.Newf("unexpected number of voters %d, expected %d",
								len(desc.Replicas().VoterDescriptors()), voters)
						}
					}
					if d.HasArg(numNonVoters) {
						var nonVoters int
						d.ScanArgs(t, numNonVoters, &nonVoters)
						if len(desc.Replicas().NonVoterDescriptors()) != nonVoters {
							return errors.Newf("unexpected number of non-voters %d, expected %d",
								len(desc.Replicas().NonVoterDescriptors()), nonVoters)
						}
					}

					return nil
				}); err != nil {
					return err.Error()
				}

			default:
				return errors.Newf("unknown command").Error()
			}
			return ""
		})
	})
}

// Constants corresponding to command-options accepted by the data-driven test.
const (
	serverIdx        = "idx"
	serverLocalities = "localities"
	tableName        = "table-name"
	numVoters        = "num-voters"
	numNonVoters     = "num-non-voters"
)

type replicaType int

const (
	replicaTypeLeaseholder replicaType = iota
	replicaTypeVoter
	replicaTypeNonVoter
	replicaTypeNotPresent
)

type datadrivenTestState struct {
	tc serverutils.TestClusterInterface
}

var replicaTypes = map[string]replicaType{
	"leaseholder": replicaTypeLeaseholder,
	"voter":       replicaTypeVoter,
	"non-voter":   replicaTypeNonVoter,
	"not-present": replicaTypeNotPresent,
}
var replicaTypeString = map[replicaType]string{
	replicaTypeLeaseholder: "leaseholder",
	replicaTypeVoter:       "voter",
	replicaTypeNonVoter:    "non-voter",
	replicaTypeNotPresent:  "not-present",
}

func (r replicaType) String() string {
	return replicaTypeString[r]
}

func (d *datadrivenTestState) cleanup(ctx context.Context) {
	if d.tc != nil {
		d.tc.Stopper().Stop(ctx)
	}
	d.tc = nil
}

func (d *datadrivenTestState) getSQLConn(idx int) (*gosql.DB, error) {
	if d.tc == nil {
		return nil, errors.New("no cluster exists")
	}
	if idx < 0 || idx >= d.tc.NumServers() {
		return nil, errors.Newf("invalid idx, must be in range [0, %d)", d.tc.NumServers())
	}
	return d.tc.ServerConn(idx), nil
}

func mustHaveArgOrFatal(t *testing.T, d *datadriven.TestData, arg string) {
	if !d.HasArg(arg) {
		t.Fatalf("no %q provided", arg)
	}
}

func nodeIdToIdx(t *testing.T, tc serverutils.TestClusterInterface, id roachpb.NodeID) int {
	for i := 0; i < tc.NumServers(); i++ {
		if tc.Server(i).NodeID() == id {
			return i
		}
	}

	t.Fatalf("could not find idx for node id %d", id)

	return -1
}

// checkReadServedLocallyInSimpleRecording looks at a "simple" trace and returns
// if the query for served locally and if it was served via a follower read.
// A "simple" trace is defined as one that contains a single "dist sender send"
// message. An error is returned if more than one (or no) "dist sender send"
// messages are found in the recording.
func checkReadServedLocallyInSimpleRecording(
	rec tracing.Recording,
) (servedLocally bool, servedUsingFollowerReads bool, err error) {
	foundDistSenderSend := false
	for _, sp := range rec {
		if sp.Operation == "dist sender send" {
			if foundDistSenderSend {
				return false,
					false,
					errors.New("recording contains > 1 dist sender send messages")
			}
			foundDistSenderSend = true
			servedLocally = tracing.LogsContainMsg(sp, kvbase.RoutingRequestLocallyMsg)
			// Check the child span to find out if the query was served using a
			// follower read.
			for _, span := range rec {
				if span.ParentSpanID == sp.SpanID {
					if tracing.LogsContainMsg(span, kvbase.FollowerReadServingMsg) {
						servedUsingFollowerReads = true
					}
				}
			}
		}
	}
	if !foundDistSenderSend {
		return false,
			false,
			errors.New("recording contains no dist sender send messages")
	}
	return servedLocally, servedUsingFollowerReads, nil
}

// replicaPlacement keeps track of which nodes have what replica types for a
// given range.
type replicaPlacement struct {
	nodeToReplicaType  map[int]replicaType
	replicaTypeToNodes map[replicaType][]int
	leaseholder        int
}

// parseReplicasFromInput constructs a replicaPlacement from user input.
func parseReplicasFromInput(
	t *testing.T, tc serverutils.TestClusterInterface, d *datadriven.TestData,
) *replicaPlacement {
	ret := replicaPlacement{}
	ret.leaseholder = -1

	userReplicas := make(map[replicaType][]int)
	for replicaTypeName := range replicaTypes {
		if !d.HasArg(replicaTypeName) {
			continue
		}

		var rawReplicas string
		d.ScanArgs(t, replicaTypeName, &rawReplicas)

		rawReplicaList := strings.Split(rawReplicas, ",")
		newReplicas := make([]int, 0, len(rawReplicaList))
		for _, rawIndex := range rawReplicaList {
			ind, err := strconv.Atoi(rawIndex)
			if err != nil {
				t.Fatalf("could not parse replica type for index: %v", err)
			}
			if ind >= tc.NumServers() || ind < 0 {
				t.Fatalf("got server index %d but have %d servers", ind, tc.NumServers())
			}
			newReplicas = append(newReplicas, ind)
		}

		userReplicas[replicaTypes[replicaTypeName]] = newReplicas
	}

	if leaseholders := userReplicas[replicaTypeLeaseholder]; len(leaseholders) == 1 {
		ret.leaseholder = leaseholders[0]
	} else if len(leaseholders) > 1 {
		t.Fatalf("got more than one leaseholder: %d", len(leaseholders))
	}

	ret.replicaTypeToNodes = userReplicas

	reversed := make(map[int]replicaType)
	for rt, replicas := range userReplicas {
		for _, replica := range replicas {
			reversed[replica] = rt
		}
	}
	ret.nodeToReplicaType = reversed

	return &ret
}

// parseReplicasFromInput constructs a replicaPlacement from a range descriptor.
func parseReplicasFromRange(
	t *testing.T, tc serverutils.TestClusterInterface, desc roachpb.RangeDescriptor,
) (*replicaPlacement, error) {
	ret := replicaPlacement{}

	replicaMap := make(map[int]replicaType)

	leaseHolder, err := tc.FindRangeLeaseHolder(desc, nil)
	if err != nil {
		return nil, errors.Newf("could not get leaseholder: %v", err)
	}
	leaseHolderIdx := nodeIdToIdx(t, tc, leaseHolder.NodeID)
	replicaMap[leaseHolderIdx] = replicaTypeLeaseholder
	ret.leaseholder = leaseHolderIdx

	for _, replica := range desc.Replicas().VoterDescriptors() {
		idx := nodeIdToIdx(t, tc, replica.NodeID)
		if _, found := replicaMap[idx]; !found {
			replicaMap[idx] = replicaTypeVoter
		}
	}

	for _, replica := range desc.Replicas().NonVoterDescriptors() {
		idx := nodeIdToIdx(t, tc, replica.NodeID)
		if rt, found := replicaMap[idx]; !found {
			replicaMap[idx] = replicaTypeNonVoter
		} else {
			return nil,
				errors.Errorf("expected not to find non-voter %d in replica map but found %s",
					idx, rt.String())
		}
	}

	ret.nodeToReplicaType = replicaMap

	reversed := make(map[replicaType][]int)
	for replica, rt := range replicaMap {
		if val, found := reversed[rt]; !found {
			reversed[rt] = []int{replica}
		} else {
			reversed[rt] = append(val, replica)
		}
	}
	ret.replicaTypeToNodes = reversed

	return &ret, nil
}

// satisfiesExpectedPlacement returns nil if the expected replicaPlacement is a
// subset of the provided replicaPlacement, error otherwise.
func (r *replicaPlacement) satisfiesExpectedPlacement(expected *replicaPlacement) error {
	for node, expectedRt := range expected.nodeToReplicaType {
		actualRt, found := r.nodeToReplicaType[node]
		if expectedRt == replicaTypeNotPresent {
			// We hope to not be able to find replicas marked as not present, so
			// continue to the next replica if that's the case.
			if !found || actualRt == replicaTypeNotPresent {
				continue
			} else {
				return errors.Newf(
					"expected node %s to not be present but had replica type %s",
					node,
					actualRt.String())
			}
		}

		if !found {
			return errors.Newf(
				"expected node %s to have replica type %s but was not found",
				node,
				expectedRt.String())
		}

		if expectedRt != actualRt {
			return errors.Newf(
				"expected node %s to have replica type %s but was %s",
				node,
				expectedRt.String(),
				actualRt.String())
		}
	}

	return nil
}

func (r *replicaPlacement) hasLeaseholderInfo() bool {
	return r.leaseholder != -1
}

func (r *replicaPlacement) getLeaseholder() int {
	return r.leaseholder
}

func (r *replicaPlacement) getReplicaType(nodeIdx int) replicaType {
	return r.nodeToReplicaType[nodeIdx]
}
