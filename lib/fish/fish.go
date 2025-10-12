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

// Package fish is the core module of the Aquarium-Fish system
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
	"github.com/adobe/aquarium-fish/lib/monitoring"
	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
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
	monitor *monitoring.Monitor

	// When the fish was started
	startup time.Time

	// Signal to stop the fish
	Quit chan os.Signal

	// Allows us to gracefully close all the subroutines
	running       context.Context //nolint:containedctx // Is used for sending stop for goroutines
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
	applicationsTimeoutsUpdated chan struct{} // Notifies about the earlier timeout then the current one

	// Keeps Temp Labels timeouts Fish watching for
	labelsTimeoutsMutex   sync.Mutex
	labelsTimeouts        map[typesv2.LabelUID]time.Time
	labelsTimeoutsUpdated chan struct{} // Notifies about the earlier timeout then the current one

	// When Application changes - fish figures that out through those channels
	applicationStateChannel chan database.ApplicationStateSubscriptionEvent
	applicationTaskChannel  chan database.ApplicationTaskSubscriptionEvent
	// Special subscription to process temporary labels removal operations
	temporaryLabelChannel chan database.LabelSubscriptionEvent

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
	ctx := context.Background()
	logger := log.WithFunc("fish", "Init")

	f.startup = time.Now()
	f.shutdownCancel = make(chan bool)
	f.Quit = make(chan os.Signal, 1)
	signal.Notify(f.Quit, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)

	// Init channel for ApplicationState changes
	f.applicationStateChannel = make(chan database.ApplicationStateSubscriptionEvent, 100)
	f.db.SubscribeApplicationState(ctx, f.applicationStateChannel)

	// Init channel for ApplicationTask changes
	f.applicationTaskChannel = make(chan database.ApplicationTaskSubscriptionEvent, 100)
	f.db.SubscribeApplicationTask(ctx, f.applicationTaskChannel)

	// Init channel for Label changes to process temporary Labels
	f.temporaryLabelChannel = make(chan database.LabelSubscriptionEvent, 100)
	f.db.SubscribeLabel(ctx, f.temporaryLabelChannel)

	// Init variables
	f.activeVotes = make(map[typesv2.ApplicationUID]*typesv2.Vote)
	f.wonVotes = make(map[typesv2.ApplicationUID]*typesv2.Vote)
	f.storageVotes = make(map[typesv2.VoteUID]typesv2.Vote)
	f.applications = make(map[typesv2.ApplicationUID]*sync.Mutex)
	f.applicationsTimeouts = make(map[typesv2.ApplicationUID]time.Time)
	f.applicationsTimeoutsUpdated = make(chan struct{})
	f.labelsTimeouts = make(map[typesv2.LabelUID]time.Time)
	f.labelsTimeoutsUpdated = make(chan struct{})

	// Set slots to 0
	var zeroSlotsValue uint32
	f.nodeUsage.Slots = &zeroSlotsValue

	f.initDefaultRoles(ctx)

	// Create admin user and ignore errors if it's existing
	_, err := f.db.UserGet(ctx, "admin")
	if err == database.ErrObjectNotFound {
		pass, adminUser, err := f.db.UserNew(ctx, "admin", "")
		if err != nil {
			logger.Error("Unable to create new admin User", "err", err)
			return fmt.Errorf("Fish: Unable to create new admin User: %v", err)
		}
		if pass != "" {
			// Print pass of newly created admin user to stderr
			// WARN: Used by integration tests
			println("Admin user pass:", pass)
		}

		// Assigning admin role
		adminUser.Roles = []string{auth.AdminRoleName}
		// Setting unlimited rate to allow admin user to properly test the system
		var rateLimit int32 = -1
		adminUser.Config = &typesv2.UserConfig{
			RateLimit: &rateLimit,
		}
		if err := f.db.UserCreate(ctx, adminUser); err != nil {
			logger.Error("Failed to create the admin user", "err", err)
			return fmt.Errorf("Fish: Failed to create the admin user: %v", err)
		}
	} else if err != nil {
		logger.Error("Unable to create admin", "err", err)
		return fmt.Errorf("Fish: Unable to create admin: %v", err)
	}

	// Init node
	createNode := false
	node, err := f.db.NodeGet(ctx, f.cfg.NodeName)
	if err != nil {
		logger.Info("Create new node", "node_name", f.cfg.NodeName, "node_location", f.cfg.NodeLocation)
		createNode = true

		node = &typesv2.Node{
			Name:     f.cfg.NodeName,
			Location: f.cfg.NodeLocation,
		}
	} else {
		logger.Info("Use existing node", "node_name", node.Name, "node_location", node.Location)
	}

	certPath := f.cfg.TLSCrt
	if !filepath.IsAbs(certPath) {
		certPath = filepath.Join(f.cfg.Directory, certPath)
	}
	if err := node.Init(f.cfg.NodeAddress, certPath); err != nil {
		return fmt.Errorf("Fish: Unable to init node: %v", err)
	}

	f.db.SetNode(node)

	if createNode {
		if err = f.db.NodeCreate(ctx, node); err != nil {
			return fmt.Errorf("Fish: Unable to create node: %v", err)
		}
	} else {
		if err = f.db.NodeSave(ctx, node); err != nil {
			return fmt.Errorf("Fish: Unable to save node: %v", err)
		}
	}
	logger.Info("Current Node UID", "node_uid", f.db.GetNodeUID())

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
	logger.Info("Using the next node identifiers", "node_identifiers", f.cfg.NodeIdentifiers)

	// Fish is running now
	f.running, f.runningCancel = context.WithCancel(context.Background())

	if err := drivers.Init(f.db, f.cfg.Directory, f.cfg.Drivers); err != nil {
		logger.Error("Unable to init drivers", "err", err)
		return fmt.Errorf("Fish: Unable to init drivers: %v", err)
	}

	// Run application state processing before resuming the assigned Applications
	go f.applicationProcess()

	logger.Debug("Resuming to execute the assigned Applications")
	resources, err := f.db.ApplicationResourceListNode(ctx, f.db.GetNodeUID())
	if err != nil {
		logger.Error("Unable to get the node resources", "err", err)
		return fmt.Errorf("Fish: Unable to get the node resources: %v", err)
	}
	for _, res := range resources {
		logger.Debug("Resuming Resource execution for Application", "app_uid", res.ApplicationUid)
		if f.db.ApplicationIsAllocated(ctx, res.ApplicationUid) == nil {
			logger.Info("Found allocated resource to serve", "res_uid", res.Uid)
			// We will not retry here, because the mentioned Applications should be already running
			if _, err := f.executeApplicationStart(res.ApplicationUid, res.DefinitionIndex); err != nil {
				f.applicationsMutex.Lock()
				delete(f.applications, res.ApplicationUid)
				f.applicationsMutex.Unlock()
				logger.Error("Can't execute Application", "app_uid", res.ApplicationUid, "err", err)
			}
		} else {
			logger.Warn("Found not allocated Resource of Application, cleaning up", "app_uid", res.ApplicationUid)
			if err := f.db.ApplicationResourceDelete(ctx, res.Uid); err != nil {
				logger.Error("Unable to delete Resource of Application", "app_uid", res.ApplicationUid, "err", err)
			}
			appState := typesv2.ApplicationState{
				ApplicationUid: res.ApplicationUid, Status: typesv2.ApplicationState_ERROR,
				Description: "Found not cleaned up resource",
			}
			f.db.ApplicationStateCreate(ctx, &appState)
		}
	}

	logger.Debug("Resuming electionProcess for the NEW and ELECTED Applications")
	electionAppStates, err := f.db.ApplicationStateListNewElected(ctx)
	if err != nil {
		logger.Error("Unable to get NEW and ELECTED ApplicationState list", "err", err)
		return fmt.Errorf("Fish: Unable to get NEW and ELECTED ApplicationState list: %v", err)
	}
	for _, as := range electionAppStates {
		appState := as
		f.maybeRunElectionProcess(&appState)
	}

	logger.Debug("Running background processes")

	// Run node ping timer
	go f.pingProcess(ctx)

	// Run ARP autoupdate process to ensure the addresses will be ok
	arp.AutoRefresh(30 * time.Second)

	// Run database cleanup & compaction process
	go f.dbCleanupCompactProcess(ctx)

	// Running the watcher for running Applications lifetime
	go f.applicationTimeoutProcess(ctx)

	// Running the watcher for temporary Labels
	go f.labelTemporaryRemoveProcess(ctx)

	return nil
}

// initDefaultRoles is needed to initialize DB with Administrator & User roles and fill-up policies
func (f *Fish) initDefaultRoles(ctx context.Context) error {
	logger := log.WithFunc("fish", "initDefaultRoles")

	// Create enforcer first since we'll need it for setting up permissions
	enforcer, err := auth.NewEnforcer()
	if err != nil {
		logger.Error("Failed to create enforcer", "err", err)
		return fmt.Errorf("Fish: Failed to create enforcer: %v", err)
	}

	// Create all roles described in the proto specs and update Aministrator role if needed
	for role, perms := range auth.GetRolePermissions() {
		r, err := f.db.RoleGet(ctx, role)
		if err != nil && err != database.ErrObjectNotFound {
			logger.Error("Unable to get role", "role", role, "err", err)
			return fmt.Errorf("Fish: Unable to get %q role: %v", role, err)
		}
		if err == database.ErrObjectNotFound {
			logger.Debug("Create role and assigning permissions", "role", role)
			newRole := typesv2.Role{
				Name:        role,
				Permissions: perms,
			}
			if err := f.db.RoleCreate(ctx, &newRole); err != nil {
				logger.Error("Failed to create role", "role", role, "err", err)
				return fmt.Errorf("Fish: Failed to create %q role: %v", role, err)
			}
		} else if role == auth.AdminRoleName && len(r.Permissions) < len(perms) {
			// NOTE: Here we can't use "!=" because that will cause issues with different versions of nodes in cluster
			logger.Debug("Updating Administrator role to reflect node changes", "role", role)
			r.Permissions = perms
			if err := f.db.RoleSave(ctx, r); err != nil {
				logger.Error("Failed to create role", "role", role, "err", err)
				return fmt.Errorf("Fish: Failed to create %q role: %v", role, err)
			}
		}
	}

	// Subscribe enforcer to role updates when they are created
	enforcerChannel := make(chan database.RoleSubscriptionEvent, 100)
	f.db.SubscribeRole(ctx, enforcerChannel)
	enforcer.SetUpdateChannel(enforcerChannel)

	// Init the existing DB role permissions to the enforcer
	roles, err := f.db.RoleList(ctx)
	if err != nil {
		logger.Error("Failed to list existing roles", "err", err)
		return fmt.Errorf("Fish: Failed to list existing roles: %v", err)
	}
	for _, r := range roles {
		for _, p := range r.Permissions {
			if err := enforcer.AddPolicy(r.Name, p.Resource, p.Action); err != nil {
				logger.Error("Failed to add role permission", "role", r.Name, "permission", p, "err", err)
				return fmt.Errorf("Fish: Failed to add %q role permission %v: %v", r, p, err)
			}
		}
	}

	return nil
}

// Close tells the node that the Fish execution need to be stopped
func (f *Fish) Close(ctx context.Context) {
	logger := log.WithFunc("fish", "Close")

	logger.Debug("Stopping the running drivers")
	if errs := drivers.Shutdown(); len(errs) > 0 {
		logger.Debug("Some drivers failed to stop", "errors", errs)
	} else {
		logger.Debug("All drivers are stopped")
	}

	f.runningCancel()
	logger.Debug("Waiting for background routines to shutdown")
	f.routines.Wait()
	logger.Debug("All the background routines are stopped")

	logger.Debug("Stopping the enforcer")
	enforcer := auth.GetEnforcer()
	if enforcer != nil {
		enforcer.Shutdown()
	}

	logger.Debug("Closing the DB")
	f.db.Shutdown(ctx)
}

// GetNode returns current Fish node spec
func (f *Fish) DB() *database.Database {
	return f.db
}

// GetCfg returns fish configuration
func (f *Fish) GetCfg() Config {
	return *f.cfg
}

func (f *Fish) pingProcess(ctx context.Context) {
	f.routinesMutex.Lock()
	f.routines.Add(1)
	f.routinesMutex.Unlock()
	defer f.routines.Done()

	logger := log.WithFunc("fish", "pingProcess")
	defer logger.Info("Fish Node: pingProcess stopped")

	// In order to optimize network & database - update just UpdatedAt field
	pingTicker := time.NewTicker(typesv2.NodePingDelay * time.Second)
	defer pingTicker.Stop()
	for {
		select {
		case <-f.running.Done():
			return
		case <-pingTicker.C:
			logger.Debug("Fish Node: ping")
			f.db.NodePing(ctx)
		}
	}
}

// applicationProcess Is used to start processes to handle ApplicationsState events
func (f *Fish) applicationProcess() {
	f.routinesMutex.Lock()
	f.routines.Add(1)
	f.routinesMutex.Unlock()
	defer f.routines.Done()

	logger := log.WithFunc("fish", "applicationProcess")
	defer logger.Info("Fish: checkApplicationProcess stopped")

	// Here we looking for all the new and executing Applications
	for {
		select {
		case <-f.running.Done():
			return
		case appStateEvent := <-f.applicationStateChannel:
			// Only process CREATED events for application states (new state changes)
			if appStateEvent.ChangeType != aquariumv2.ChangeType_CHANGE_TYPE_CREATED {
				continue
			}
			appState := appStateEvent.Object
			switch appState.Status {
			case typesv2.ApplicationState_UNSPECIFIED:
				logger.Error("Application has unspecified state", "app_uid", appState.ApplicationUid, "app_status", appState.Status)
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
				// f.maybeRunApplicationTask(appState.ApplicationUid, nil)
				logger.Debug("Application reached end state", "app_uid", appState.ApplicationUid, "app_status", appState.Status)
			}
		case appTaskEvent := <-f.applicationTaskChannel:
			// Only process CREATED events for application tasks (new tasks) or UPDATED events
			if appTaskEvent.ChangeType != aquariumv2.ChangeType_CHANGE_TYPE_CREATED && appTaskEvent.ChangeType != aquariumv2.ChangeType_CHANGE_TYPE_UPDATED {
				continue
			}
			appTask := appTaskEvent.Object
			// Runs check for Application state and decides if need to execute or drop
			// If the Application state doesn't fit the task - then it will be skipped to be
			// started later by the ApplicationState change event
			f.maybeRunApplicationTask(appTask.ApplicationUid, appTask)
		}
	}
}

// dbCleanupCompactProcess background process helping with managing the database cleannes
func (f *Fish) dbCleanupCompactProcess(ctx context.Context) {
	f.routinesMutex.Lock()
	f.routines.Add(1)
	f.routinesMutex.Unlock()
	defer f.routines.Done()

	logger := log.WithFunc("fish", "dbCleanupCompactProcess")
	defer logger.Info("Completed")

	// Checking the completed/error applications and clean up if they've sit there for > 5 minutes
	dbCleanupDelay := time.Duration(f.cfg.DBCleanupInterval)
	cleanupTicker := time.NewTicker(dbCleanupDelay / 2)
	defer cleanupTicker.Stop()
	logger.Info("Triggering CleanupDB once per", "interval", dbCleanupDelay/2)

	dbCompactDelay := time.Duration(f.cfg.DBCompactInterval)
	compactionTicker := time.NewTicker(dbCompactDelay)
	logger.Info("Triggering CompactDB once per", "interval", dbCompactDelay)
	defer compactionTicker.Stop()

	for {
		select {
		case <-f.running.Done():
			return
		case <-cleanupTicker.C:
			f.CleanupDB(ctx)
		case <-compactionTicker.C:
			f.db.CompactDB(ctx)
		}
	}
}

// CleanupDB removing stale Applications and data from database to keep it slim
func (f *Fish) CleanupDB(ctx context.Context) {
	logger := log.WithFunc("fish", "CleanupDB")

	logger.Debug("CleanupDB running")
	// WARN: Used by integration tests
	defer logger.Debug("Completed", "cleanupdb", "completed")

	// Detecting the time we need to use as a cutting point
	dbCleanupDelay := time.Duration(f.cfg.DBCleanupInterval)
	cutTime := time.Now().Add(-dbCleanupDelay)

	// Look for the stale Applications
	states, err := f.db.ApplicationStateListLatest(ctx)
	if err != nil {
		logger.Warn("Unable to get ApplicationStates", "err", err)
		return
	}
	for _, state := range states {
		if !f.db.ApplicationStateIsDead(state.Status) {
			continue
		}
		logger.Debug("Checking Application", "app_uid", state.ApplicationUid, "app_status", state.Status, "created_at", state.CreatedAt)

		if state.CreatedAt.After(cutTime) {
			logger.Debug("Skipping due to not reached the cut time", "app_uid", state.ApplicationUid, "time_left", state.CreatedAt.Sub(cutTime))
			continue
		}

		// If the Application died before the Fish is started - then we need to give it aditional dbCleanupDelay time
		if f.startup.After(cutTime) {
			logger.Debug("Skipping due to recent startup", "app_uid", state.ApplicationUid, "time_left", f.startup.Sub(cutTime))
			continue
		}

		logger.Debug("Removing everything related to Application", "app_uid", state.ApplicationUid, "app_status", state.Status)

		// First of all removing the Application itself to make sure it will not be restarted
		if err = f.db.ApplicationDelete(ctx, state.ApplicationUid); err != nil {
			logger.Error("Unable to remove Application", "app_uid", state.ApplicationUid, "err", err)
			continue
		}

		ats, _ := f.db.ApplicationTaskListByApplication(ctx, state.ApplicationUid)
		for _, at := range ats {
			if err = f.db.ApplicationTaskDelete(ctx, at.Uid); err != nil {
				logger.Error("Unable to remove ApplicationTask", "task_uid", at.Uid, "err", err)
			}
		}

		ss, _ := f.db.ApplicationStateListByApplication(ctx, state.ApplicationUid)
		for _, s := range ss {
			if err = f.db.ApplicationStateDelete(ctx, s.Uid); err != nil {
				logger.Error("Unable to remove ApplicationState", "state_uid", s.Uid, "err", err)
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
	logger := log.WithFunc("fish", "isNodeAvailableForDefinition")

	// When node is in maintenance mode - it should not accept any Applications
	if f.maintenance {
		logger.Debug("Maintenance mode blocks node availability")
		return false
	}

	// Is node supports the required label driver
	driver := drivers.GetProvider(def.Driver)
	if driver == nil {
		logger.Debug("No driver found with name", "driver", def.Driver)
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
				logger.Debug("Not enough slots to execute definition", "needed_slots", neededSlots, "node_slots_limit", f.cfg.NodeSlotsLimit)
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
				logger.Debug("NodeFilter prevents to run on this node", "filter", needed)
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
		logger.Warn("AvailableCapacity of driver took too long", "driver", def.Driver, "elapsed", elapsed)
	}
	if capacity < 1 {
		logger.Debug("Driver has not enough capacity", "driver", driver.Name(), "capacity", capacity)
		return false
	}

	return true
}

// MaintenanceSet sets/unsets the maintenance mode which will not allow to accept the additional Applications
func (f *Fish) MaintenanceSet(value bool) {
	logger := log.WithFunc("fish", "MaintenanceSet")

	if f.maintenance != value {
		if value {
			logger.Info("Enabled maintenance mode, no new workload accepted")
		} else {
			logger.Info("Disabled maintenance mode, accepting new workloads")
		}
	}

	f.maintenance = value
}

// ShutdownSet tells node it need to execute graceful shutdown operation
func (f *Fish) ShutdownSet(value bool) {
	logger := log.WithFunc("fish", "ShutdownSet")

	if f.shutdown != value {
		if value {
			f.activateShutdown()
		} else {
			logger.Info("Disabled shutdown mode")
			f.shutdownCancel <- true
		}
	}

	f.shutdown = value
}

// ShutdownDelaySet set of how much time to wait before executing the node shutdown operation
func (f *Fish) ShutdownDelaySet(delay time.Duration) {
	logger := log.WithFunc("fish", "ShutdownDelaySet")

	if f.shutdownDelay != delay {
		logger.Info("Shutdown delay is set", "delay", delay)
	}

	f.shutdownDelay = delay
}

func (f *Fish) activateShutdown() {
	logger := log.WithFunc("fish", "activateShutdown")

	logger.Info("Enabled shutdown mode", "maintenance", f.maintenance, "delay", f.shutdownDelay)

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
				logger.Debug("Apps execution completed")
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
				logger.Info("Countdown", "time_until", time.Until(delayEndTime))
			case <-delayTimer.C:
				// Delay time has passed, triggering shutdown
				fireShutdown <- true
			case <-fireShutdown:
				logger.Info("Send quit signal to Fish")
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
					logger.Debug("Checking apps execution", "apps_count", appsCount)
					if appsCount == 0 {
						waitApps <- true
						return
					}
				case <-tickerReport.C:
					f.applicationsMutex.Lock()
					appsCount := len(f.applications)
					f.applicationsMutex.Unlock()
					logger.Info("Waiting for running Applications", "apps_count", appsCount)
				}
			}
		}()
	} else {
		// Sending signal since no need to wait for the apps
		waitApps <- true
	}
}

// SetMonitor sets the monitoring instance for the Fish node
func (f *Fish) SetMonitor(monitor *monitoring.Monitor) {
	f.monitor = monitor
}

// GetMonitor returns the monitoring instance for instrumentation
func (f *Fish) GetMonitor() *monitoring.Monitor {
	return f.monitor
}
