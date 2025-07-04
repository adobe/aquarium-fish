/**
 * Copyright 2021-2025 Adobe. All rights reserved.
 * This file is licensed to you under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License. You may obtain a copy
 * of the License at http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
 * OF ANY KIND, either express or implied. See the License for the specific language
 * governing permissions and limitations under the License.
 */

// Author: Sergei Parshev (@sparshev)

package fish

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/mostlygeek/arp"

	"github.com/adobe/aquarium-fish/lib/auth"
	"github.com/adobe/aquarium-fish/lib/database"
	"github.com/adobe/aquarium-fish/lib/drivers"
	"github.com/adobe/aquarium-fish/lib/log"
	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
)

// ClusterInterface defines required functions for Fish to run on the cluster
type ClusterInterface interface {
	// Requesting send of Vote to cluster, since it's not a part of DB
	SendVote(vote *typesv2.Vote) error
}

// Fish structure is used to store the node internal state
type Fish struct {
	db      *database.Database
	cfg     *Config
	cluster ClusterInterface

	// When the fish was started
	startup time.Time

	// Signal to stop the fish
	Quit chan os.Signal

	// Allows us to gracefully close all the subroutines
	running       context.Context //nolint:containedctx
	runningCancel context.CancelFunc
	routines      sync.WaitGroup
	routinesMutex sync.Mutex

	maintenance    bool
	shutdown       bool
	shutdownCancel chan bool
	shutdownDelay  time.Duration

	// Storage for the current Node Votes participating in election process
	activeVotesMutex sync.RWMutex
	activeVotes      map[typesv2.ApplicationUID]*typesv2.Vote

	// Used to temporary store the won Votes by Application UID to tell node to run execution
	wonVotesMutex sync.Mutex
	wonVotes      map[typesv2.ApplicationUID]*typesv2.Vote

	// Votes of the other nodes in the cluster
	storageVotesMutex sync.RWMutex
	storageVotes      map[typesv2.VoteUID]typesv2.Vote

	// Stores the currently executing Applications and their state transition locks
	applicationsMutex sync.Mutex
	applications      map[typesv2.ApplicationUID]*sync.Mutex

	// Keeps Applications timeouts Fish watching for
	applicationsTimeoutsMutex   sync.Mutex
	applicationsTimeouts        map[typesv2.ApplicationUID]time.Time
	applicationsTimeoutsUpdated chan struct{} // Notifies about the earlier timeout then exists

	// When Application changes - fish figures that out through those channels
	applicationStateChannel chan *typesv2.ApplicationState
	applicationTaskChannel  chan *typesv2.ApplicationTask

	// Stores the current usage of the node resources
	nodeUsageMutex sync.Mutex // Is needed to protect node resources from concurrent allocations
	nodeUsage      typesv2.Resources
}

// New creates new Fish node
func New(db *database.Database, cfg *Config) (*Fish, error) {
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

	// Init channel for ApplicationState changes
	f.applicationStateChannel = make(chan *typesv2.ApplicationState)
	f.db.SubscribeApplicationState(f.applicationStateChannel)

	// Init channel for ApplicationTask changes
	f.applicationTaskChannel = make(chan *typesv2.ApplicationTask)
	f.db.SubscribeApplicationTask(f.applicationTaskChannel)

	// Init variables
	f.activeVotes = make(map[typesv2.ApplicationUID]*typesv2.Vote)
	f.wonVotes = make(map[typesv2.ApplicationUID]*typesv2.Vote)
	f.storageVotes = make(map[typesv2.VoteUID]typesv2.Vote)
	f.applications = make(map[typesv2.ApplicationUID]*sync.Mutex)
	f.applicationsTimeouts = make(map[typesv2.ApplicationUID]time.Time)
	f.applicationsTimeoutsUpdated = make(chan struct{})

	// Set slots to 0
	var zeroSlotsValue uint32
	f.nodeUsage.Slots = &zeroSlotsValue

	f.initDefaultRoles()

	// Create admin user and ignore errors if it's existing
	_, err := f.db.UserGet("admin")
	if err == database.ErrObjectNotFound {
		pass, adminUser, err := f.db.UserNew("admin", "")
		if err != nil {
			return log.Error("Fish: Unable to create new admin User:", err)
		}
		if pass != "" {
			// Print pass of newly created admin user to stderr
			println("Admin user pass:", pass)
		}

		// Assigning admin role
		adminUser.Roles = []string{auth.AdminRoleName}
		if err := f.db.UserSave(adminUser); err != nil {
			return log.Error("Fish: Failed to assign Administrator Role to admin user:", err)
		}
	} else if err != nil {
		return log.Error("Fish: Unable to create admin:", err)
	}

	// Init node
	createNode := false
	node, err := f.db.NodeGet(f.cfg.NodeName)
	if err != nil {
		log.Info("Fish: Create new node:", f.cfg.NodeName, f.cfg.NodeLocation)
		createNode = true

		node = &typesv2.Node{
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

	f.db.SetNode(*node)

	if createNode {
		if err = f.db.NodeCreate(f.db.GetNode()); err != nil {
			return fmt.Errorf("Fish: Unable to create node: %v", err)
		}
	} else {
		if err = f.db.NodeSave(f.db.GetNode()); err != nil {
			return fmt.Errorf("Fish: Unable to save node: %v", err)
		}
	}

	// Fill the node identifiers with defaults
	if len(f.cfg.NodeIdentifiers) == 0 {
		// Capturing the current host identifiers
		f.cfg.NodeIdentifiers = append(f.cfg.NodeIdentifiers, "FishName:"+node.Name,
			"HostName:"+node.Definition.Host.Hostname,
			"OS:"+node.Definition.Host.Os,
			"OSVersion:"+node.Definition.Host.PlatformVersion,
			"OSPlatform:"+node.Definition.Host.Platform,
			"OSFamily:"+node.Definition.Host.PlatformFamily,
			"Arch:"+node.Definition.Host.KernelArch,
		)
	}
	log.Info("Fish: Using the next node identifiers:", f.cfg.NodeIdentifiers)

	// Fish is running now
	f.running, f.runningCancel = context.WithCancel(context.Background())

	if err := drivers.Init(f.db, f.cfg.Directory, f.cfg.Drivers); err != nil {
		return log.Error("Fish: Unable to init drivers:", err)
	}

	// Run application state processing before resuming the assigned Applications
	go f.applicationProcess()

	log.Debug("Fish: Resuming to execute the assigned Applications...")
	resources, err := f.db.ApplicationResourceListNode(f.db.GetNodeUID())
	if err != nil {
		return log.Error("Fish: Unable to get the node resources:", err)
	}
	for _, res := range resources {
		log.Debugf("Fish: Resuming Resource execution for Application: %q", res.ApplicationUid)
		if f.db.ApplicationIsAllocated(res.ApplicationUid) == nil {
			log.Info("Fish: Found allocated resource to serve:", res.Uid)
			// We will not retry here, because the mentioned Applications should be already running
			if _, err := f.executeApplicationStart(res.ApplicationUid, res.DefinitionIndex); err != nil {
				f.applicationsMutex.Lock()
				delete(f.applications, res.ApplicationUid)
				f.applicationsMutex.Unlock()
				log.Errorf("Fish: Can't execute Application %s: %v", res.ApplicationUid, err)
			}
		} else {
			log.Warn("Fish: Found not allocated Resource of Application, cleaning up:", res.ApplicationUid)
			if err := f.db.ApplicationResourceDelete(res.Uid); err != nil {
				log.Error("Fish: Unable to delete Resource of Application:", res.ApplicationUid, err)
			}
			appState := typesv2.ApplicationState{
				ApplicationUid: res.ApplicationUid, Status: typesv2.ApplicationState_ERROR,
				Description: "Found not cleaned up resource",
			}
			f.db.ApplicationStateCreate(&appState)
		}
	}

	log.Debug("Fish: Resuming electionProcess for the NEW and ELECTED Applications...")
	electionAppStates, err := f.db.ApplicationStateListNewElected()
	if err != nil {
		return log.Error("Fish: Unable to get NEW and ELECTED ApplicationState list:", err)
	}
	for _, as := range electionAppStates {
		appState := as
		f.maybeRunElectionProcess(&appState)
	}

	log.Debug("Fish: Running background processes...")

	// Run node ping timer
	go f.pingProcess()

	// Run ARP autoupdate process to ensure the addresses will be ok
	arp.AutoRefresh(30 * time.Second)

	// Run database cleanup & compaction process
	go f.dbCleanupCompactProcess()

	// Running the watcher for running Applications lifetime
	go f.applicationTimeoutProcess()

	return nil
}

// initDefaultRoles is needed to initialize DB with Administrator & User roles and fill-up policies
func (f *Fish) initDefaultRoles() error {
	// TODO: Implement enforcer update on role change
	// Create enforcer first since we'll need it for setting up permissions
	enforcer, err := auth.NewEnforcer()
	if err != nil {
		return log.Error("Fish: Failed to create enforcer:", err)
	}

	// Create all roles described in the proto specs
	for role, perms := range auth.GetRolePermissions() {
		newRole := typesv2.Role{
			Name:        role,
			Permissions: perms,
		}

		r, err := f.db.RoleGet(role)
		if err == database.ErrObjectNotFound {
			log.Debugf("Fish: Create %q role and assigning permissions", role)
			r = &newRole
			if err := f.db.RoleCreate(r); err != nil {
				return log.Errorf("Fish: Failed to create %q role: %v", role, err)
			}
		} else if err != nil {
			return log.Errorf("Fish: Unable to get %q role: %v", role, err)
		}

		// Add role permissions to the enforcer
		for _, p := range r.Permissions {
			if err := enforcer.AddPolicy(r.Name, p.Resource, p.Action); err != nil {
				return log.Errorf("Fish: Failed to add %q role permission %v: %v", role, p, err)
			}
		}
	}

	return nil
}

// Close tells the node that the Fish execution need to be stopped
func (f *Fish) Close() {
	log.Debug("Fish: Stopping the running drivers")
	if errs := drivers.Shutdown(); len(errs) > 0 {
		log.Debugf("Fish: Some drivers failed to stop: %v", errs)
	} else {
		log.Debug("Fish: All drivers are stopped")
	}

	f.runningCancel()
	log.Debug("Fish: Waiting for background routines to shutdown")
	f.routines.Wait()
	log.Debug("Fish: All the background routines are stopped")

	log.Debug("Fish: Closing the DB")
	f.db.Shutdown()
}

// GetNode returns current Fish node spec
func (f *Fish) DB() *database.Database {
	return f.db
}

// GetCfg returns fish configuration
func (f *Fish) GetCfg() Config {
	return *f.cfg
}

func (f *Fish) pingProcess() {
	f.routinesMutex.Lock()
	f.routines.Add(1)
	f.routinesMutex.Unlock()
	defer f.routines.Done()
	defer log.Info("Fish Node: pingProcess stopped")

	// In order to optimize network & database - update just UpdatedAt field
	pingTicker := time.NewTicker(typesv2.NodePingDelay * time.Second)
	defer pingTicker.Stop()
	for {
		select {
		case <-f.running.Done():
			return
		case <-pingTicker.C:
			log.Debug("Fish Node: ping")
			f.db.NodePing(f.db.GetNode())
		}
	}
}

// applicationProcess Is used to start processes to handle ApplicationsState events
func (f *Fish) applicationProcess() {
	f.routinesMutex.Lock()
	f.routines.Add(1)
	f.routinesMutex.Unlock()
	defer f.routines.Done()
	defer log.Info("Fish: checkApplicationProcess stopped")

	// Here we looking for all the new and executing Applications
	for {
		select {
		case <-f.running.Done():
			return
		case appState := <-f.applicationStateChannel:
			switch appState.Status {
			case typesv2.ApplicationState_UNSPECIFIED:
				log.Errorf("Fish: Application %s has unspecified state %s", appState.ApplicationUid, appState.Status)
			case typesv2.ApplicationState_NEW:
				// Running election process for the new Application, if it's not already procesing
				f.maybeRunElectionProcess(appState)
			case typesv2.ApplicationState_ELECTED:
				// Starting Application execution if we are winners of the election
				f.maybeRunExecuteApplicationStart(appState)
			case typesv2.ApplicationState_ALLOCATED:
				// Executing deallocation procedures for the Application
				f.maybeRunApplicationTask(appState.ApplicationUid, nil)
			case typesv2.ApplicationState_DEALLOCATE:
				// Executing deallocation procedures for the Application
				f.maybeRunExecuteApplicationStop(appState)
			case typesv2.ApplicationState_DEALLOCATED, typesv2.ApplicationState_ERROR:
				// Not much to do here, but maybe later in the future?
				// In this state the Application has no Resource to deal with, so no tasks for now
				//f.maybeRunApplicationTask(appState.ApplicationUid, nil)
				log.Debugf("Fish: Application %s reached end state %s", appState.ApplicationUid, appState.Status)
			}
		case appTask := <-f.applicationTaskChannel:
			// Runs check for Application state and decides if need to execute or drop
			// If the Application state doesn't fit the task - then it will be skipped to be
			// started later by the ApplicationState change event
			f.maybeRunApplicationTask(appTask.ApplicationUid, appTask)
		}
	}
}

// dbCleanupCompactProcess background process helping with managing the database cleannes
func (f *Fish) dbCleanupCompactProcess() {
	f.routinesMutex.Lock()
	f.routines.Add(1)
	f.routinesMutex.Unlock()
	defer f.routines.Done()
	defer log.Info("Fish: dbCleanupCompactProcess stopped")

	// Checking the completed/error applications and clean up if they've sit there for > 5 minutes
	dbCleanupDelay := time.Duration(f.cfg.DBCleanupInterval)
	cleanupTicker := time.NewTicker(dbCleanupDelay / 2)
	defer cleanupTicker.Stop()
	log.Infof("Fish: dbCleanupCompactProcess: Triggering CleanupDB once per %s", dbCleanupDelay/2)

	dbCompactDelay := time.Duration(f.cfg.DBCompactInterval)
	compactionTicker := time.NewTicker(dbCompactDelay)
	log.Infof("Fish: dbCleanupCompactProcess: Triggering CompactDB once per %s", dbCompactDelay)
	defer compactionTicker.Stop()

	for {
		select {
		case <-f.running.Done():
			return
		case <-cleanupTicker.C:
			f.CleanupDB()
		case <-compactionTicker.C:
			f.db.CompactDB()
		}
	}
}

// CleanupDB removing stale Applications and data from database to keep it slim
func (f *Fish) CleanupDB() {
	log.Debug("Fish: CleanupDB running...")
	defer log.Debug("Fish: CleanupDB completed")

	// Detecting the time we need to use as a cutting point
	dbCleanupDelay := time.Duration(f.cfg.DBCleanupInterval)
	cutTime := time.Now().Add(-dbCleanupDelay)

	// Look for the stale Applications
	states, err := f.db.ApplicationStateListLatest()
	if err != nil {
		log.Warnf("Fish: CleanupDB: Unable to get ApplicationStates: %v", err)
		return
	}
	for _, state := range states {
		if !f.db.ApplicationStateIsDead(state.Status) {
			continue
		}
		log.Debugf("Fish: CleanupDB: Checking Application %s (%s): %v", state.ApplicationUid, state.Status, state.CreatedAt)

		if state.CreatedAt.After(cutTime) {
			log.Debugf("Fish: CleanupDB: Skipping %s due to not reached the cut time, left: %s", state.ApplicationUid, state.CreatedAt.Sub(cutTime))
			continue
		}

		// If the Application died before the Fish is started - then we need to give it aditional dbCleanupDelay time
		if f.startup.After(cutTime) {
			log.Debugf("Fish: CleanupDB: Skipping %s due to recent startup, left: %s", state.ApplicationUid, f.startup.Sub(cutTime))
			continue
		}

		log.Debugf("Fish: CleanupDB: Removing everything related to Application %s (%s)", state.ApplicationUid, state.Status)

		// First of all removing the Application itself to make sure it will not be restarted
		if err = f.db.ApplicationDelete(state.ApplicationUid); err != nil {
			log.Errorf("Fish: CleanupDB: Unable to remove Application %s: %v", state.ApplicationUid, err)
			continue
		}

		ats, _ := f.db.ApplicationTaskListByApplication(state.ApplicationUid)
		for _, at := range ats {
			if err = f.db.ApplicationTaskDelete(at.Uid); err != nil {
				log.Errorf("Fish: CleanupDB: Unable to remove ApplicationTask %s: %v", at.Uid, err)
			}
		}

		ss, _ := f.db.ApplicationStateListByApplication(state.ApplicationUid)
		for _, s := range ss {
			if err = f.db.ApplicationStateDelete(s.Uid); err != nil {
				log.Errorf("Fish: CleanupDB: Unable to remove ApplicationState %s: %v", s.Uid, err)
			}
		}
	}
}

func (f *Fish) isNodeAvailableForDefinitions(defs []typesv2.LabelDefinition) int {
	available := -1 // Set "nope" answer by default in case all the definitions are not fit
	for i, def := range defs {
		if f.isNodeAvailableForDefinition(def) {
			available = i
			break
		}
	}

	return available
}

func (f *Fish) isNodeAvailableForDefinition(def typesv2.LabelDefinition) bool {
	// When node is in maintenance mode - it should not accept any Applications
	if f.maintenance {
		log.Debug("Fish: Maintenance mode blocks node availability")
		return false
	}

	// Is node supports the required label driver
	driver := drivers.GetProvider(def.Driver)
	if driver == nil {
		log.Debugf("Fish: No driver found with name %q", def.Driver)
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
				var val uint32 = 1
				def.Resources.Slots = &val
			}
			neededSlots := (*f.nodeUsage.Slots) + (*def.Resources.Slots)
			if uint(neededSlots) > f.cfg.NodeSlotsLimit {
				log.Debugf("Fish: Not enough slots to execute definition: %d > %d", neededSlots, f.cfg.NodeSlotsLimit)
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
				log.Debugf("Fish: NodeFilter prevents to run on this node: %q", needed)
				return false
			}
		}
	}
	// Here all the node filters matched the node identifiers

	// Check with the driver if it's possible to allocate the Application resource
	nodeUsage := f.nodeUsage
	before := time.Now()
	capacity := driver.AvailableCapacity(nodeUsage, def)
	elapsed := time.Since(before)
	if elapsed > 300*time.Millisecond {
		log.Warnf("Fish: AvailableCapacity of %s driver took %s", def.Driver, elapsed)
	}
	if capacity < 1 {
		log.Debugf("Fish: Driver %q has not enough capacity: %d", driver.Name(), capacity)
		return false
	}

	return true
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
					f.applicationsMutex.Lock()
					appsCount := len(f.applications)
					f.applicationsMutex.Unlock()
					log.Debug("Fish: Shutdown: checking apps execution:", appsCount)
					if appsCount == 0 {
						waitApps <- true
						return
					}
				case <-tickerReport.C:
					f.applicationsMutex.Lock()
					appsCount := len(f.applications)
					f.applicationsMutex.Unlock()
					log.Info("Fish: Shutdown: waiting for running Applications:", appsCount)
				}
			}
		}()
	} else {
		// Sending signal since no need to wait for the apps
		waitApps <- true
	}
}
