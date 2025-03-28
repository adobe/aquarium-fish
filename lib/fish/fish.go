/**
 * Copyright 2021 Adobe. All rights reserved.
 * This file is licensed to you under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License. You may obtain a copy
 * of the License at http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
 * OF ANY KIND, either express or implied. See the License for the specific language
 * governing permissions and limitations under the License.
 */

package fish

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/mostlygeek/arp"
	"go.mills.io/bitcask/v2"

	"github.com/adobe/aquarium-fish/lib/drivers"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
	"github.com/adobe/aquarium-fish/lib/util"
)

// ElectionRoundTime defines how long the voting round will take in seconds - so cluster nodes will be able to interchange their responses
const ElectionRoundTime = 30

// ClusterInterface defines required functions for Fish to run on the cluster
type ClusterInterface interface {
	// Requesting send of Vote to cluster, since it's not a part of DB
	SendVote(vote *types.Vote) error
}

// Fish structure is used to store the node internal state
type Fish struct {
	db      *bitcask.Bitcask
	cfg     *Config
	node    *types.Node
	cluster ClusterInterface

	// When the fish was started
	startup time.Time

	// Signal to stop the fish
	Quit chan os.Signal

	// Allows us to gracefully close all the subroutines
	running       context.Context //nolint:containedctx
	runningCancel context.CancelFunc
	routines      sync.WaitGroup

	maintenance    bool
	shutdown       bool
	shutdownCancel chan bool
	shutdownDelay  time.Duration

	activeVotesMutex sync.RWMutex
	activeVotes      map[types.ApplicationUID]types.Vote

	// Votes of the other nodes in the cluster
	storageVotesMutex sync.RWMutex
	storageVotes      map[types.VoteUID]types.Vote

	// Stores the currently executing Applications
	applicationsMutex sync.RWMutex
	applications      []types.ApplicationUID

	// Used to temporary store the won Votes by Application create time
	wonVotesMutex sync.Mutex
	wonVotes      []types.Vote

	// Stores the current usage of the node resources
	nodeUsageMutex sync.Mutex // Is needed to protect node resources from concurrent allocations
	nodeUsage      types.Resources
}

// New creates new Fish node
func New(db *bitcask.Bitcask, cfg *Config) (*Fish, error) {
	f := &Fish{db: db, cfg: cfg}
	if err := f.Init(); err != nil {
		return nil, err
	}

	return f, nil
}

// Init initializes the Fish node
func (f *Fish) Init() error {
	f.startup = time.Now()
	f.shutdownCancel = make(chan bool)
	f.Quit = make(chan os.Signal, 1)
	signal.Notify(f.Quit, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)

	// Init variables
	f.activeVotes = make(map[types.ApplicationUID]types.Vote)
	f.storageVotes = make(map[types.VoteUID]types.Vote)

	// Set slots to 0
	var val uint = 0
	f.nodeUsage.Slots = &val

	// Create admin user and ignore errors if it's existing
	_, err := f.UserGet("admin")
	if err == bitcask.ErrObjectNotFound {
		if pass, _, _ := f.UserNew("admin", ""); pass != "" {
			// Print pass of newly created admin user to stderr
			println("Admin user pass:", pass)
		}
	} else if err != nil {
		return log.Error("Fish: Unable to create admin:", err)
	}

	// Init node
	createNode := false
	node, err := f.NodeGet(f.cfg.NodeName)
	if err != nil {
		log.Info("Fish: Create new node:", f.cfg.NodeName, f.cfg.NodeLocation)
		createNode = true

		node = &types.Node{
			Name:     f.cfg.NodeName,
			Location: f.cfg.NodeLocation,
		}
	} else {
		log.Info("Fish: Use existing node:", node.Name, node.Location)
	}

	certPath := f.cfg.TLSCrt
	if !filepath.IsAbs(certPath) {
		certPath = filepath.Join(f.cfg.Directory, certPath)
	}
	if err := node.Init(f.cfg.NodeAddress, certPath); err != nil {
		return fmt.Errorf("Fish: Unable to init node: %v", err)
	}

	f.node = node
	if createNode {
		if err = f.NodeCreate(f.node); err != nil {
			return fmt.Errorf("Fish: Unable to create node: %v", err)
		}
	} else {
		if err = f.NodeSave(f.node); err != nil {
			return fmt.Errorf("Fish: Unable to save node: %v", err)
		}
	}

	// Fill the node identifiers with defaults
	if len(f.cfg.NodeIdentifiers) == 0 {
		// Capturing the current host identifiers
		f.cfg.NodeIdentifiers = append(f.cfg.NodeIdentifiers, "FishName:"+node.Name,
			"HostName:"+node.Definition.Host.Hostname,
			"OS:"+node.Definition.Host.OS,
			"OSVersion:"+node.Definition.Host.PlatformVersion,
			"OSPlatform:"+node.Definition.Host.Platform,
			"OSFamily:"+node.Definition.Host.PlatformFamily,
			"Arch:"+node.Definition.Host.KernelArch,
		)
	}
	log.Info("Fish: Using the next node identifiers:", f.cfg.NodeIdentifiers)

	// Fish is running now
	f.running, f.runningCancel = context.WithCancel(context.Background())

	if err := f.driversSet(); err != nil {
		return log.Error("Fish: Unable to set drivers:", err)
	}
	if errs := f.driversPrepare(f.cfg.Drivers); errs != nil {
		log.Error("Fish: Unable to prepare some resource drivers:", errs)
	}

	// Continue to execute the assigned applications
	resources, err := f.ApplicationResourceListNode(f.node.UID)
	if err != nil {
		return log.Error("Fish: Unable to get the node resources:", err)
	}
	for _, res := range resources {
		if f.ApplicationIsAllocated(res.ApplicationUID) == nil {
			log.Info("Fish: Found allocated resource to serve:", res.UID)
			if err := f.executeApplication(res.ApplicationUID, res.DefinitionIndex); err != nil {
				log.Errorf("Fish: Can't execute Application %s: %v", res.ApplicationUID, err)
			}
		} else {
			log.Warn("Fish: Found not allocated Resource of Application, cleaning up:", res.ApplicationUID)
			if err := f.ApplicationResourceDelete(res.UID); err != nil {
				log.Error("Fish: Unable to delete Resource of Application:", res.ApplicationUID, err)
			}
			appState := &types.ApplicationState{ApplicationUID: res.ApplicationUID, Status: types.ApplicationStatusERROR,
				Description: "Found not cleaned up resource",
			}
			f.ApplicationStateCreate(appState)
		}
	}

	// Run node ping timer
	go f.pingProcess()

	// Run application vote process
	go f.checkNewApplicationProcess()

	// Run ARP autoupdate process to ensure the addresses will be ok
	arp.AutoRefresh(30 * time.Second)

	// Run database cleanup & compaction process
	go f.dbCleanupCompactProcess()

	return nil
}

// Close tells the node that the Fish execution need to be stopped
func (f *Fish) Close() {
	f.runningCancel()
	log.Debug("Fish: Waiting for background routines to shutdown")
	f.routines.Wait()
	log.Debug("Fish: All the background routines are stopped")

	log.Debug("Fish: Compacting & closing the DB")
	f.CompactDB()
	f.db.Close()
}

// GetNodeUID returns node UID
func (f *Fish) GetNodeUID() types.ApplicationUID {
	return f.node.UID
}

// GetNode returns Fish node spec
func (f *Fish) GetNode() *types.Node {
	return f.node
}

// GetCfg returns fish configuration
func (f *Fish) GetCfg() Config {
	return *f.cfg
}

// NewUID Creates new UID with 6 starting bytes of Node UID as prefix
func (f *Fish) NewUID() uuid.UUID {
	uid := uuid.New()
	copy(uid[:], f.node.UID[:6])
	return uid
}

// GetLocation returns node location
func (f *Fish) GetLocation() string {
	return f.node.Location
}

func (f *Fish) checkNewApplicationProcess() {
	f.routines.Add(1)
	defer f.routines.Done()
	defer log.Info("Fish: checkNewApplicationProcess stopped")

	checkTicker := time.NewTicker(5 * time.Second)
	for {
		select {
		case <-f.running.Done():
			return
		case <-checkTicker.C:
			// Check new apps available for processing
			newApps, err := f.ApplicationListGetStatusNew()
			if err != nil {
				log.Error("Fish: Unable to get NEW ApplicationState list:", err)
				continue
			}
			for _, app := range newApps {
				// Check if Vote is already here
				if _, err := f.activeVotesGet(app.UID); err == nil {
					continue
				}
				log.Info("Fish: NEW Application with no Vote:", app.UID, app.CreatedAt)

				// Vote not exists in the active votes - running the process
				// We need to keep this mutex here to ensure vote is put into active votes to not
				// process it next time accidentally
				f.activeVotesMutex.Lock()
				{
					// Create new Vote and run background vote process
					f.activeVotes[app.UID] = f.VoteCreate(app.UID)
					go f.electionProcess(app.UID)
				}
				f.activeVotesMutex.Unlock()
			}

			// Check the Applications ready to be allocated
			// It's needed to be single-threaded to have some order in allocation - FIFO principle,
			// who won first should be processed first.
			f.wonVotesMutex.Lock()
			toProcess := []types.Vote{}
			toProcess = append(toProcess, f.wonVotes...)
			f.wonVotesMutex.Unlock()

			if len(toProcess) > 0 {
				log.Debug("Fish: Processing the Applications to allocate:", len(toProcess))

				before := time.Now()
				for _, v := range toProcess {
					if err := f.executeApplication(v.ApplicationUID, v.Available); err != nil {
						log.Errorf("Fish: Can't execute Application %s: %v", v.ApplicationUID, err)
					}
					f.wonVotesRemove(v.UID)
				}
				elapsed := time.Since(before)

				if elapsed > 10*time.Second {
					log.Warnf("Fish: %d Applications allocation took %s", len(toProcess), elapsed)
				}
			}
		}
	}
}

// dbCleanupCompactProcess background process helping with managing the database cleannes
func (f *Fish) dbCleanupCompactProcess() {
	f.routines.Add(1)
	defer f.routines.Done()
	defer log.Info("Fish: dbCleanupCompactProcess stopped")

	// Checking the completed/error applications and clean up if they've sit there for > 5 minutes
	dbCleanupDelay, err := time.ParseDuration(f.cfg.DBCleanupDelay)
	if err != nil {
		dbCleanupDelay = DefaultDBCleanupDelay
		log.Errorf("Fish: dbCleanupCompactProcess: Delay is set incorrectly in fish config, using default %s: %v", dbCleanupDelay, err)
	}
	cleanupTicker := time.NewTicker(dbCleanupDelay / 2)
	log.Infof("Fish: dbCleanupCompactProcess: Triggering CleanupDB once per %s", dbCleanupDelay/2)

	compactionTicker := time.NewTicker(time.Hour)
	log.Infof("Fish: dbCleanupCompactProcess: Triggering CompactDB once per %s", time.Hour)

	for {
		select {
		case <-f.running.Done():
			return
		case <-cleanupTicker.C:
			f.CleanupDB()
		case <-compactionTicker.C:
			f.CompactDB()
		}
	}
}

// CleanupDB removing stale Applications and data from database to keep it slim
func (f *Fish) CleanupDB() {
	log.Debug("Fish: CleanupDB running...")
	defer log.Debug("Fish: CleanupDB completed")

	// Detecting the time we need to use as a cutting point
	dbCleanupDelay, err := time.ParseDuration(f.cfg.DBCleanupDelay)
	if err != nil {
		dbCleanupDelay = DefaultDBCleanupDelay
		log.Errorf("Fish: CleanupDB: Delay is set incorrectly in fish config, using default %s: %v", dbCleanupDelay, err)
	}

	cutTime := time.Now().Add(-dbCleanupDelay)

	// Look for the stale Applications
	states, err := f.ApplicationStateListLatest()
	if err != nil {
		log.Warnf("Fish: CleanupDB: Unable to get ApplicationStates: %v", err)
		return
	}
	for _, state := range states {
		if state.Status != types.ApplicationStatusERROR && state.Status != types.ApplicationStatusDEALLOCATED {
			continue
		}
		log.Debugf("Fish: CleanupDB: Checking Application %s (%s): %v, %v", state.UID, state.Status, state.CreatedAt, cutTime)

		if state.CreatedAt.After(cutTime) {
			continue
		}

		// If the Application died before the Fish is started - then we need to give it aditional dbCleanupDelay time
		if state.CreatedAt.Before(f.startup.Add(dbCleanupDelay)) {
			continue
		}

		log.Debugf("Fish: CleanupDB: Removing everything related to Application %s (%s)", state.ApplicationUID, state.Status)

		// First of all removing the Application itself to make sure it will not be restarted
		if err = f.ApplicationDelete(state.ApplicationUID); err != nil {
			log.Errorf("Fish: CleanupDB: Unable to remove Application %s: %v", state.ApplicationUID, err)
			continue
		}

		ats, _ := f.ApplicationTaskListByApplication(state.ApplicationUID)
		for _, at := range ats {
			if err = f.ApplicationTaskDelete(at.UID); err != nil {
				log.Errorf("Fish: CleanupDB: Unable to remove ApplicationTask %s: %v", at.UID, err)
			}
		}

		sms, _ := f.ServiceMappingListByApplication(state.ApplicationUID)
		for _, sm := range sms {
			if err = f.ServiceMappingDelete(sm.UID); err != nil {
				log.Errorf("Fish: CleanupDB: Unable to remove ServiceMapping %s: %v", sm.UID, err)
			}
		}

		ss, _ := f.ApplicationStateListByApplication(state.ApplicationUID)
		for _, s := range ss {
			if err = f.ApplicationStateDelete(s.UID); err != nil {
				log.Errorf("Fish: CleanupDB: Unable to remove ApplicationState %s: %v", s.UID, err)
			}
		}
	}
}

// CompactDB runs stale Applications and data removing
func (f *Fish) CompactDB() {
	log.Debug("Fish: CompactDB running...")
	defer log.Debug("Fish: CompactDB done")

	s, _ := f.db.Stats()
	log.Debugf("Fish: CompactDB: Before compaction: Datafiles: %d, Keys: %d, Size: %d, Reclaimable: %d", s.Datafiles, s.Keys, s.Size, s.Reclaimable)

	f.db.Merge()

	s, _ = f.db.Stats()
	log.Debugf("Fish: CompactDB: After compaction: Datafiles: %d, Keys: %d, Size: %d, Reclaimable: %d", s.Datafiles, s.Keys, s.Size, s.Reclaimable)
}

// electionProcess performs & monitors the election process for the new Application until the exec
// node will be elected.
func (f *Fish) electionProcess(appUID types.ApplicationUID) error {
	vote, err := f.activeVotesGet(appUID)
	if err != nil {
		return log.Errorf("Fish: Election %q: Fatal: Unable to get the Vote for Application: %v", appUID, err)
	}
	// Make sure the active vote will be removed in case error happens to restart the process next time
	defer f.activeVotesRemove(appUID)

	app, err := f.ApplicationGet(appUID)
	if err != nil {
		return log.Errorf("Fish: Election %q: Fatal: Unable to get the Application: %v", appUID, err)
	}

	// Get label with the definitions
	label, err := f.LabelGet(app.LabelUID)
	if err != nil {
		return log.Errorf("Fish: Election %q: Fatal: Unable to get the Label %s: %v", appUID, app.LabelUID, err)
	}

	// Loop to reiterate each new round
	for {
		// Set the round based on the time of Application creation
		vote.Round = f.voteCurrentRoundGet(app.CreatedAt)

		log.Infof("Fish: Election %q: Starting Application election round %d", appUID, vote.Round)

		// Determine answer for this round, it will try find the first possible definition to serve
		vote.Available = f.isNodeAvailableForDefinitions(label.Definitions)

		// Sync vote with the other nodes
		if vote.UID == uuid.Nil {
			if err := f.clusterVoteSend(vote); err != nil {
				return log.Errorf("Fish: Election %q: Fatal: Unable to sync vote: %v", appUID, err)
			}
		}

		// Calculating the end time of the round to not stuck if some nodes are not available
		roundEndsAt := app.CreatedAt.Add(time.Duration(ElectionRoundTime*(vote.Round+1)) * time.Second)

		// Loop to recheck status within the round
		for time.Until(roundEndsAt) > 0 {
			// Check all the cluster nodes voted
			nodes, err := f.NodeActiveList()
			if err != nil {
				return log.Errorf("Fish: Election %q: Fatal: Unable to get the Node list: %v", appUID, err)
			}
			votes := f.voteListGetApplicationRound(appUID, vote.Round)
			if err != nil {
				return log.Errorf("Fish: Election %q: Fatal: Unable to get the Vote list: %v", appUID, err)
			}
			if len(votes) >= len(nodes) {
				// Ok, all nodes voted so let's move to election
				bestVote := types.Vote{}
				for _, v := range votes {
					// Available must be >= 0, otherwise the node is not available to execute this Application
					if v.Available < 0 {
						continue
					}
					// If there is no best one - set this one as best to compare the others with it
					if bestVote.UID == uuid.Nil {
						bestVote = v
						continue
					}

					// Now comparing the rest of the votes with the best one. The system here is simple:
					// When we have equal values for both votes - we getting down to the next filter.
					// Rarely corner case will happen when even rand will show equal values - then the
					// round becomes failed and we try the next one.
					if v.Available > bestVote.Available {
						continue
					} else if v.Available == bestVote.Available {
						if v.RuleResult < bestVote.RuleResult {
							continue
						} else if v.RuleResult == bestVote.RuleResult {
							if v.Rand < bestVote.Rand {
								continue
							} else if v.Rand == bestVote.Rand {
								log.Warnf("Fish: Election %q: This round is a lucky one! Rands are equal for nodes %s and %s", appUID, v.NodeUID, bestVote.NodeUID)
								bestVote.UID = uuid.Nil
								break
							}
						}
					}

					// It seems the current one vote is better then the best one, so replacing
					bestVote = v
				}

				// Checking the best vote
				if bestVote.UID == uuid.Nil {
					log.Infof("Fish: Election %q: No candidates in round %d", appUID, vote.Round)
				} else if bestVote.NodeUID == f.node.UID {
					log.Infof("Fish: Election %q: I won the election", appUID)
					f.wonVotesAdd(bestVote, app.CreatedAt)
				} else {
					log.Infof("Fish: Election %q: I lost the election to Node %s", appUID, vote.NodeUID)
				}

				// Wait till the next round
				// Doesn't matter what's the result of the round - we need to wait till the next one
				// anyway to check if the Application was served or run another round
				time.Sleep(time.Until(roundEndsAt))

				// Check if the Application changed state
				if s, err := f.ApplicationStateGetByApplication(appUID); err != nil {
					log.Errorf("Fish: Election %q: Unable to get the Application state: %v", appUID, err)
					// The Application state is not found, so we can drop the election process
					f.activeVotesRemove(vote.UID)
					f.storageVotesCleanup()
					return nil
				} else if s.Status != types.ApplicationStatusNEW {
					// The Application state was changed by some node, so we can drop the election process
					f.activeVotesRemove(vote.UID)
					f.storageVotesCleanup()
					return nil
				}

				// Next round seems needed
				vote.UID = uuid.Nil
				break
			}

			log.Debugf("Fish: Election %q: Some nodes didn't vote (%d >= %d), waiting till %v...", appUID, len(votes), len(nodes), roundEndsAt)

			// Wait 5 sec and repeat
			time.Sleep(5 * time.Second)
		}
	}
}

func (f *Fish) isNodeAvailableForDefinitions(defs []types.LabelDefinition) int {
	available := -1 // Set "nope" answer by default in case all the definitions are not fit
	for i, def := range defs {
		if f.isNodeAvailableForDefinition(def) {
			available = i
			break
		}
	}

	return available
}

func (f *Fish) isNodeAvailableForDefinition(def types.LabelDefinition) bool {
	// When node is in maintenance mode - it should not accept any Applications
	if f.maintenance {
		return false
	}

	// Is node supports the required label driver
	driver := f.driverGet(def.Driver)
	if driver == nil {
		return false
	}

	// If the driver is using the local resources - we need to lock them to reduce the possibility
	// of conflict with allocation process. Remote drivers implements their way to lock.
	if !driver.IsRemote() {
		f.nodeUsageMutex.Lock()
		defer f.nodeUsageMutex.Unlock()

		// Processing node slots only if the limit is set
		if f.cfg.NodeSlotsLimit > 0 {
			// Use 1 by default for the definitions where slots value is not set
			if def.Resources.Slots == nil {
				var val uint = 1
				def.Resources.Slots = &val
			}
			if (*f.nodeUsage.Slots)+(*def.Resources.Slots) > f.cfg.NodeSlotsLimit {
				return false
			}
		}
	}

	// Verify node filters because some workload can't be running on all the physical nodes
	// The node becomes fitting only when all the needed node filter patterns are matched
	if len(def.Resources.NodeFilter) > 0 {
		neededIdents := def.Resources.NodeFilter
		currentIdents := f.cfg.NodeIdentifiers
		for _, needed := range neededIdents {
			found := false
			for _, value := range currentIdents {
				// We're validating the pattern on error during label creation, so they should be ok
				if found, _ = path.Match(needed, value); found {
					break
				}
			}
			if !found {
				// One of the required node identifiers did not matched the node ones
				return false
			}
		}
	}
	// Here all the node filters matched the node identifiers

	// Check with the driver if it's possible to allocate the Application resource
	nodeUsage := f.nodeUsage
	if capacity := driver.AvailableCapacity(nodeUsage, def); capacity < 1 {
		return false
	}

	return true
}

func (f *Fish) executeApplication(appUID types.ApplicationUID, defIndex int) error {
	// Check the application is executed already
	f.applicationsMutex.RLock()
	{
		for _, uid := range f.applications {
			if uid == appUID {
				// Seems the application is already executing
				f.applicationsMutex.RUnlock()
				return nil
			}
		}
	}
	f.applicationsMutex.RUnlock()

	// Make sure definition is >= 0 which means it was chosen by the node
	if defIndex < 0 {
		return fmt.Errorf("Fish: The definition index for Application %s is not chosen: %v", appUID, defIndex)
	}

	app, err := f.ApplicationGet(appUID)
	if err != nil {
		return fmt.Errorf("Fish: Unable to get the Application: %v", err)
	}

	// Check current Application state
	appState, err := f.ApplicationStateGetByApplication(app.UID)
	if err != nil {
		return fmt.Errorf("Fish: Unable to get the Application state: %v", err)
	}

	// Get label with the definitions
	label, err := f.LabelGet(app.LabelUID)
	if err != nil {
		return fmt.Errorf("Fish: Unable to find Label %s: %v", app.LabelUID, err)
	}

	// Extract the Label Definition by the provided index
	if len(label.Definitions) <= defIndex {
		return fmt.Errorf("Fish: ERROR: The chosen Definition not exists in the Label %s: %v (App: %s)", app.LabelUID, defIndex, app.UID)
	}
	labelDef := label.Definitions[defIndex]

	// The already running applications will not consume the additional resources
	if appState.Status == types.ApplicationStatusNEW {
		// In case there is multiple Applications won the election process on the same node it could
		// just have not enough resources, so skip it for now to allow the other Nodes to try again.
		if !f.isNodeAvailableForDefinition(labelDef) {
			log.Warn("Fish: Not enough resources to execute the Application", app.UID)
			return nil
		}
	}

	// Locate the required driver
	driver := f.driverGet(labelDef.Driver)
	if driver == nil {
		return fmt.Errorf("Fish: Unable to locate driver for the Application %s: %s", app.UID, labelDef.Driver)
	}

	// If the driver is not using the remote resources - we need to increase the counter
	if !driver.IsRemote() {
		f.nodeUsageMutex.Lock()
		f.nodeUsage.Add(labelDef.Resources)
		f.nodeUsageMutex.Unlock()
	}

	// Adding the application to list
	f.applicationsMutex.Lock()
	f.applications = append(f.applications, app.UID)
	f.applicationsMutex.Unlock()

	// The main application processing is executed on background because allocation could take a
	// while, after that the bg process will wait for application state change. We do not separate
	// it into method because effectively it could not be running without the logic above.
	go func() {
		log.Info("Fish: Start executing Application", app.UID, appState.Status)

		if appState.Status == types.ApplicationStatusNEW {
			// Set Application state as ELECTED
			appState = &types.ApplicationState{ApplicationUID: app.UID, Status: types.ApplicationStatusELECTED,
				Description: "Elected node: " + f.node.Name,
			}
			err := f.ApplicationStateCreate(appState)
			if err != nil {
				log.Error("Fish: Unable to set Application state:", app.UID, err)
				f.applicationsMutex.Lock()
				f.removeFromExecutingApplincations(app.UID)
				f.applicationsMutex.Unlock()
				return
			}
		}

		// Merge application and label metadata, in this exact order
		var mergedMetadata []byte
		var metadata map[string]any
		if err := json.Unmarshal([]byte(app.Metadata), &metadata); err != nil {
			log.Error("Fish: Unable to parse the Application metadata:", app.UID, err)
			appState = &types.ApplicationState{ApplicationUID: app.UID, Status: types.ApplicationStatusERROR,
				Description: fmt.Sprint("Unable to parse the app metadata:", err),
			}
			f.ApplicationStateCreate(appState)
		}
		if err := json.Unmarshal([]byte(label.Metadata), &metadata); err != nil {
			log.Error("Fish: Unable to parse the Label metadata:", label.UID, err)
			appState = &types.ApplicationState{ApplicationUID: app.UID, Status: types.ApplicationStatusERROR,
				Description: fmt.Sprint("Unable to parse the label metadata:", err),
			}
			f.ApplicationStateCreate(appState)
		}
		if mergedMetadata, err = json.Marshal(metadata); err != nil {
			log.Error("Fish: Unable to merge metadata:", label.UID, err)
			appState = &types.ApplicationState{ApplicationUID: app.UID, Status: types.ApplicationStatusERROR,
				Description: fmt.Sprint("Unable to merge metadata:", err),
			}
			f.ApplicationStateCreate(appState)
		}

		// Get or create the new resource object
		res := &types.ApplicationResource{
			ApplicationUID: app.UID,
			NodeUID:        f.node.UID,
			Metadata:       util.UnparsedJSON(mergedMetadata),
		}
		if appState.Status == types.ApplicationStatusALLOCATED {
			res, err = f.ApplicationResourceGetByApplication(app.UID)
			if err != nil {
				log.Error("Fish: Unable to get the allocated Resource for Application:", app.UID, err)
				appState = &types.ApplicationState{ApplicationUID: app.UID, Status: types.ApplicationStatusERROR,
					Description: fmt.Sprint("Unable to find the allocated resource:", err),
				}
				f.ApplicationStateCreate(appState)
			}
		}

		// Allocate the resource
		if appState.Status == types.ApplicationStatusELECTED {
			// Run the allocation
			log.Infof("Fish: Allocate the Application %s with label %q definition %d resource using driver: %s", app.UID, label.Name, defIndex, driver.Name())
			drvRes, err := driver.Allocate(labelDef, metadata)
			if err != nil {
				log.Error("Fish: Unable to allocate resource for the Application:", app.UID, err)
				appState = &types.ApplicationState{ApplicationUID: app.UID, Status: types.ApplicationStatusERROR,
					Description: fmt.Sprint("Driver allocate resource error:", err),
				}
			} else {
				res.Identifier = drvRes.Identifier
				res.HwAddr = drvRes.HwAddr
				res.IpAddr = drvRes.IpAddr
				res.LabelUID = label.UID
				res.DefinitionIndex = defIndex
				res.Authentication = drvRes.Authentication
				err := f.ApplicationResourceCreate(res)
				if err != nil {
					log.Error("Fish: Unable to store Resource for Application:", app.UID, err)
				}
				appState = &types.ApplicationState{ApplicationUID: app.UID, Status: types.ApplicationStatusALLOCATED,
					Description: "Driver allocated the resource",
				}
				log.Infof("Fish: Allocated Resource %q for the Application %s", app.UID, res.Identifier)
			}
			f.ApplicationStateCreate(appState)
		}

		// Getting the resource lifetime to know how much time it will live
		resourceLifetime, err := time.ParseDuration(labelDef.Resources.Lifetime)
		if labelDef.Resources.Lifetime != "" && err != nil {
			log.Error("Fish: Can't parse the Lifetime from Label Definition:", label.UID, res.DefinitionIndex)
		}
		if err != nil {
			// Try to get default value from fish config
			resourceLifetime, err = time.ParseDuration(f.cfg.DefaultResourceLifetime)
			if err != nil {
				// Not an error - in worst case the resource will just sit there but at least will
				// not ruin the workload execution
				log.Warn("Fish: Default Resource Lifetime is not set in fish config")
			}
		}
		resourceTimeout := res.CreatedAt.Add(resourceLifetime)
		if appState.Status == types.ApplicationStatusALLOCATED {
			if resourceLifetime > 0 {
				log.Infof("Fish: Resource of Application %s will be deallocated by timeout in %s (%s)", app.UID, resourceLifetime, resourceTimeout)
			} else {
				log.Warn("Fish: Resource have no lifetime set and will live until deallocated by user:", app.UID)
			}
		}

		// Run the loop to wait for deallocate request
		var deallocateRetry uint8 = 1
		for appState.Status == types.ApplicationStatusALLOCATED {
			select {
			case <-f.running.Done():
				log.Info("Fish: Dropping the Application execution:", app.UID)
				return
			default:
				appState, err = f.ApplicationStateGetByApplication(app.UID)
				if err != nil {
					log.Error("Fish: Unable to get Status for Application:", app.UID, err)
				}

				// Check if it's life timeout for the resource
				if resourceLifetime > 0 {
					// The time limit is set - so let's use resource create time and find out timeout
					if resourceTimeout.Before(time.Now()) {
						// Seems the timeout has come, so fish asks for application deallocate
						appState = &types.ApplicationState{ApplicationUID: app.UID, Status: types.ApplicationStatusDEALLOCATE,
							Description: fmt.Sprint("Resource lifetime timeout reached:", resourceLifetime),
						}
						f.ApplicationStateCreate(appState)
					}
				}

				// Execute the existing ApplicationTasks. It will be executed during ALLOCATED or prior
				// to executing deallocation by DEALLOCATE & RECALLED which right now is useful for
				// `snapshot` and `image` tasks.
				f.executeApplicationTasks(driver, &labelDef, res, appState.Status)

				if appState.Status == types.ApplicationStatusDEALLOCATE || appState.Status == types.ApplicationStatusRECALLED {
					log.Info("Fish: Running Deallocate of the Application and Resource:", app.UID, res.Identifier)
					// Deallocating and destroy the resource
					if err := driver.Deallocate(res); err != nil {
						log.Errorf("Fish: Unable to deallocate the Resource of Application: %s (try: %d): %v", app.UID, deallocateRetry, err)
						// Let's retry to deallocate the resource 10 times before give up
						if deallocateRetry <= 10 {
							deallocateRetry++
							time.Sleep(10 * time.Second)
							continue
						}
						appState = &types.ApplicationState{ApplicationUID: app.UID, Status: types.ApplicationStatusERROR,
							Description: fmt.Sprint("Driver deallocate resource error:", err),
						}
					} else {
						log.Info("Fish: Successful deallocation of the Application:", app.UID)
						appState = &types.ApplicationState{ApplicationUID: app.UID, Status: types.ApplicationStatusDEALLOCATED,
							Description: "Driver deallocated the resource",
						}
					}
					// Destroying the resource anyway to not bloat the table - otherwise it will stuck there and
					// will block the access to IP of the other VM's that will reuse this IP
					if err := f.ApplicationResourceDelete(res.UID); err != nil {
						log.Error("Fish: Unable to delete Resource for Application:", app.UID, err)
					}
					f.ApplicationStateCreate(appState)
				} else {
					time.Sleep(5 * time.Second)
				}
			}
		}

		f.applicationsMutex.Lock()
		{
			// Decrease the amout of running local apps
			if !driver.IsRemote() {
				f.nodeUsageMutex.Lock()
				f.nodeUsage.Subtract(labelDef.Resources)
				f.nodeUsageMutex.Unlock()
			}

			// Clean the executing application
			f.removeFromExecutingApplincations(app.UID)
		}
		f.applicationsMutex.Unlock()

		log.Info("Fish: Done executing Application", app.UID, appState.Status)
	}()

	return nil
}

func (f *Fish) executeApplicationTasks(drv drivers.ResourceDriver, def *types.LabelDefinition, res *types.ApplicationResource, appStatus types.ApplicationStatus) error {
	// Execute the associated ApplicationTasks if there is some
	tasks, err := f.ApplicationTaskListByApplicationAndWhen(res.ApplicationUID, appStatus)
	if err != nil {
		return log.Error("Fish: Unable to get ApplicationTasks:", res.ApplicationUID, err)
	}
	for _, task := range tasks {
		// Skipping already executed task
		if task.Result != "{}" {
			continue
		}
		t := drv.GetTask(task.Task, string(task.Options))
		if t == nil {
			log.Error("Fish: Unable to get associated driver task type for Application:", res.ApplicationUID, task.Task)
			task.Result = util.UnparsedJSON(`{"error":"task not available in driver"}`)
		} else {
			// Executing the task
			t.SetInfo(&task, def, res)
			result, err := t.Execute()
			if err != nil {
				// We're not crashing here because even with error task could have a result
				log.Error("Fish: Error happened during executing the task:", task.UID, err)
			}
			task.Result = util.UnparsedJSON(result)
		}
		if err := f.ApplicationTaskSave(&task); err != nil {
			log.Error("Fish: Error during update the task with result:", task.UID, err)
		}
	}

	return nil
}

func (f *Fish) removeFromExecutingApplincations(appUID types.ApplicationUID) {
	for i, uid := range f.applications {
		if uid != appUID {
			continue
		}
		f.applications[i] = f.applications[len(f.applications)-1]
		f.applications = f.applications[:len(f.applications)-1]
		break
	}
}

func (f *Fish) activeVotesGet(appUID types.ApplicationUID) (*types.Vote, error) {
	f.activeVotesMutex.RLock()
	defer f.activeVotesMutex.RUnlock()

	if vote, ok := f.activeVotes[appUID]; ok {
		return &vote, nil
	}
	return nil, fmt.Errorf("Fish: Unable to find the Application vote")
}

// activeVotesRemove completes the voting process by removing active Vote from the list
func (f *Fish) activeVotesRemove(appUID types.ApplicationUID) {
	f.activeVotesMutex.Lock()
	defer f.activeVotesMutex.Unlock()

	delete(f.activeVotes, appUID)
}

// wonVotesAdd will add won vote to the list in order of Application CreatedAt
func (f *Fish) wonVotesAdd(vote types.Vote, appCreatedAt time.Time) {
	f.wonVotesMutex.Lock()
	defer f.wonVotesMutex.Unlock()
	f.wonVotes = append(f.wonVotes, vote)
	for i, v := range f.wonVotes {
		if app, err := f.ApplicationGet(v.ApplicationUID); err != nil || app.CreatedAt.Before(appCreatedAt) {
			continue
		}
		copy(f.wonVotes[i+1:], f.wonVotes[i:])
		f.wonVotes[i] = vote
		break
	}
}

func (f *Fish) wonVotesRemove(voteUID types.VoteUID) {
	f.wonVotesMutex.Lock()
	defer f.wonVotesMutex.Unlock()
	wv := f.wonVotes
	for i, v := range f.wonVotes {
		if v.UID != voteUID {
			continue
		}
		wv[i] = wv[len(wv)-1]
		f.wonVotes = wv[:len(wv)-1]
		break
	}
}

func (*Fish) voteCurrentRoundGet(appCreatedAt time.Time) uint16 {
	// In order to not start round too late - adding 1 second for processing, sending and syncing.
	// Otherwise if the node is just started and the round is almost completed - there is no use
	// to participate in the current round.
	return uint16((time.Since(appCreatedAt).Seconds() + 1) / ElectionRoundTime)
}

// StorageVotesAdd puts received votes from the cluster to the list
func (f *Fish) StorageVotesAdd(votes []types.Vote) {
	f.storageVotesMutex.Lock()
	defer f.storageVotesMutex.Unlock()

	for _, vote := range votes {
		if err := vote.Validate(); err != nil {
			log.Errorf("Fish: Unable to validate Vote from Node %s: %v", vote.NodeUID, err)
			continue
		}
		// Check the storage already holds the vote UID
		if _, ok := f.storageVotes[vote.UID]; ok {
			continue
		}
		f.storageVotes[vote.UID] = vote
	}
}

// storageVotesCleanup is running when Application becomes allocated to leave there only active
func (f *Fish) storageVotesCleanup() {
	// Getting a list of active Votes ApplicationUID's to quickly get through during filter
	f.activeVotesMutex.RLock()
	activeApps := make(map[types.ApplicationUID]uint16, len(f.activeVotes))
	for _, v := range f.activeVotes {
		activeApps[v.ApplicationUID] = v.Round
	}
	f.activeVotesMutex.RUnlock()

	// Filtering storageVotes list
	f.storageVotesMutex.Lock()
	defer f.storageVotesMutex.Unlock()

	var found bool
	for voteUID, vote := range f.storageVotes {
		found = false
		for appUID, round := range activeApps {
			if vote.ApplicationUID == appUID && vote.Round == round {
				found = true
				break
			}
		}
		if !found {
			delete(f.storageVotes, voteUID)
		}
	}
}

// MaintenanceSet sets/unsets the maintenance mode which will not allow to accept the additional Applications
func (f *Fish) MaintenanceSet(value bool) {
	if f.maintenance != value {
		if value {
			log.Info("Fish: Enabled maintenance mode, no new workload accepted")
		} else {
			log.Info("Fish: Disabled maintenance mode, accepting new workloads")
		}
	}

	f.maintenance = value
}

// ShutdownSet tells node it need to execute graceful shutdown operation
func (f *Fish) ShutdownSet(value bool) {
	if f.shutdown != value {
		if value {
			f.activateShutdown()
		} else {
			log.Info("Fish: Disabled shutdown mode")
			f.shutdownCancel <- true
		}
	}

	f.shutdown = value
}

// ShutdownDelaySet set of how much time to wait before executing the node shutdown operation
func (f *Fish) ShutdownDelaySet(delay time.Duration) {
	if f.shutdownDelay != delay {
		log.Info("Fish: Shutdown delay is set to:", delay)
	}

	f.shutdownDelay = delay
}

func (f *Fish) activateShutdown() {
	log.Infof("Fish: Enabled shutdown mode with maintenance: %v, delay: %v", f.maintenance, f.shutdownDelay)

	waitApps := make(chan bool, 1)

	// Running the main shutdown routine
	go func() {
		fireShutdown := make(chan bool, 1)
		delayTickerReport := &time.Ticker{}
		delayTimer := &time.Timer{}
		var delayEndTime time.Time

		for {
			select {
			case <-f.shutdownCancel:
				return
			case <-waitApps:
				// Maintenance mode: All the apps are completed so it's safe to shutdown
				log.Debug("Fish: Shutdown: apps execution completed")
				// If the delay is set, then running timer to execute shutdown with delay
				if f.shutdownDelay > 0 {
					delayEndTime = time.Now().Add(f.shutdownDelay)
					delayTickerReport = time.NewTicker(30 * time.Second)
					delayTimer = time.NewTimer(f.shutdownDelay)

					// Those defers will be executed just once, so no issues with loop & defer
					defer delayTickerReport.Stop()
					defer delayTimer.Stop()
				} else {
					// No delay is needed, so shutdown now
					fireShutdown <- true
				}
			case <-delayTickerReport.C:
				log.Infof("Fish: Shutdown: countdown: T-%v", time.Until(delayEndTime))
			case <-delayTimer.C:
				// Delay time has passed, triggering shutdown
				fireShutdown <- true
			case <-fireShutdown:
				log.Info("Fish: Shutdown sends quit signal to Fish")
				f.Quit <- syscall.SIGQUIT
			}
		}
	}()

	if f.maintenance {
		// Running wait for unfinished apps go routine
		go func() {
			tickerCheck := time.NewTicker(2 * time.Second)
			defer tickerCheck.Stop()
			tickerReport := time.NewTicker(30 * time.Second)
			defer tickerReport.Stop()

			for {
				select {
				case <-f.shutdownCancel:
					return
				case <-tickerCheck.C:
					// Need to make sure we're not executing any workload
					log.Debug("Fish: Shutdown: checking apps execution:", len(f.applications))
					if len(f.applications) == 0 {
						waitApps <- true
						return
					}
				case <-tickerReport.C:
					log.Info("Fish: Shutdown: waiting for running Applications:", len(f.applications))
				}
			}
		}()
	} else {
		// Sending signal since no need to wait for the apps
		waitApps <- true
	}
}
