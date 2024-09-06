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
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/mostlygeek/arp"
	"gorm.io/gorm"

	"github.com/adobe/aquarium-fish/lib/drivers"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
	"github.com/adobe/aquarium-fish/lib/util"
)

const ElectionRoundTime = 30

type Fish struct {
	db   *gorm.DB
	cfg  *Config
	node *types.Node

	// Signal to stop the fish
	Quit chan os.Signal

	running        bool
	maintenance    bool
	shutdown       bool
	shutdownCancel chan bool
	shutdownDelay  time.Duration

	activeVotesMutex sync.Mutex
	activeVotes      []*types.Vote

	// Stores the currently executing Applications
	applicationsMutex sync.Mutex
	applications      []types.ApplicationUID

	// Used to temporary store the won Votes by Application create time
	wonVotesMutex sync.Mutex
	wonVotes      map[int64]types.Vote

	// Stores the current usage of the node resources
	nodeUsageMutex sync.Mutex // Is needed to protect node resources from concurrent allocations
	nodeUsage      types.Resources
}

func New(db *gorm.DB, cfg *Config) (*Fish, error) {
	f := &Fish{db: db, cfg: cfg}
	if err := f.Init(); err != nil {
		return nil, err
	}

	return f, nil
}

func (f *Fish) Init() error {
	f.shutdownCancel = make(chan bool)
	f.Quit = make(chan os.Signal, 1)
	signal.Notify(f.Quit, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)

	if err := f.db.AutoMigrate(
		&types.User{},
		&types.Node{},
		&types.Label{},
		&types.Application{},
		&types.ApplicationState{},
		&types.ApplicationTask{},
		&types.Resource{},
		&types.ResourceAccess{},
		&types.Vote{},
		&types.Location{},
		&types.ServiceMapping{},
	); err != nil {
		return fmt.Errorf("Fish: Unable to apply DB schema: %v", err)
	}

	// Init variables
	f.wonVotes = make(map[int64]types.Vote, 5)

	// Create admin user and ignore errors if it's existing
	_, err := f.UserGet("admin")
	if err == gorm.ErrRecordNotFound {
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
			Name: f.cfg.NodeName,
		}
		if f.cfg.NodeLocation != "" {
			loc, err := f.LocationGetByName(f.cfg.NodeLocation)
			if err != nil {
				log.Info("Fish: Creating new location:", f.cfg.NodeLocation)
				loc.Name = f.cfg.NodeLocation
				loc.Description = fmt.Sprintf("Created automatically during node '%s' startup", f.cfg.NodeName)
				if f.LocationCreate(loc) != nil {
					return fmt.Errorf("Fish: Unable to create new location")
				}
			}
			node.LocationName = loc.Name
		}
	} else {
		log.Info("Fish: Use existing node:", node.Name, node.LocationName)
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
	f.running = true

	if err := f.DriversSet(); err != nil {
		return log.Error("Fish: Unable to set drivers:", err)
	}
	if errs := f.DriversPrepare(f.cfg.Drivers); errs != nil {
		log.Error("Fish: Unable to prepare some resource drivers:", errs)
	}

	// Continue to execute the assigned applications
	resources, err := f.ResourceListNode(f.node.UID)
	if err != nil {
		return log.Error("Fish: Unable to get the node resources:", err)
	}
	for _, res := range resources {
		if f.ApplicationIsAllocated(res.ApplicationUID) == nil {
			log.Info("Fish: Found allocated resource to serve:", res.UID)
			vote, err := f.VoteGetNodeApplication(f.node.UID, res.ApplicationUID)
			if err != nil {
				log.Errorf("Fish: Can't find Application vote %s: %v", res.ApplicationUID, err)
				continue
			}
			if err := f.executeApplication(*vote); err != nil {
				log.Errorf("Fish: Can't execute Application %s: %v", vote.ApplicationUID, err)
			}
		} else {
			log.Warn("Fish: Found not allocated Resource of Application, cleaning up:", res.ApplicationUID)
			if err := f.ResourceDelete(res.UID); err != nil {
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

	return nil
}

func (f *Fish) Close() {
	f.running = false
}

func (f *Fish) GetNodeUID() types.ApplicationUID {
	return f.node.UID
}

func (f *Fish) GetNode() *types.Node {
	return f.node
}

// Creates new UID with 6 starting bytes of Node UID as prefix
func (f *Fish) NewUID() uuid.UUID {
	uid := uuid.New()
	copy(uid[:], f.node.UID[:6])
	return uid
}

func (f *Fish) GetLocationName() types.LocationName {
	return f.node.LocationName
}

func (f *Fish) checkNewApplicationProcess() {
	checkTicker := time.NewTicker(5 * time.Second)
	for {
		if !f.running {
			break
		}
		// TODO: Here should be select with quit in case app is stopped to not wait next ticker
		<-checkTicker.C
		{
			// Check new apps available for processing
			newApps, err := f.ApplicationListGetStatusNew()
			if err != nil {
				log.Error("Fish: Unable to get NEW ApplicationState list:", err)
				continue
			}
			for _, app := range newApps {
				// Check if Vote is already here
				if f.voteActive(app.UID) {
					continue
				}
				log.Info("Fish: NEW Application with no vote:", app.UID, app.CreatedAt)

				// Vote not exists in the active votes - running the process
				f.activeVotesMutex.Lock()
				{
					// Check if it's already exist in the DB (if node was restarted during voting)
					vote, _ := f.VoteGetNodeApplication(f.node.UID, app.UID)

					// Ensure the app & node is set in the vote
					vote.ApplicationUID = app.UID
					vote.NodeUID = f.node.UID

					f.activeVotes = append(f.activeVotes, vote)
					go f.voteProcessRound(vote)
				}
				f.activeVotesMutex.Unlock()
			}

			// Check the Applications ready to be allocated
			// It's needed to be single-threaded to have some order in allocation - FIFO principle,
			// who requested first should be processed first.
			f.wonVotesMutex.Lock()
			{
				// We need to sort the won_votes by key which is time they was created
				keys := make([]int64, 0, len(f.wonVotes))
				for k := range f.wonVotes {
					keys = append(keys, k)
				}
				sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

				for _, k := range keys {
					if err := f.executeApplication(f.wonVotes[k]); err != nil {
						log.Errorf("Fish: Can't execute Application %s: %v", f.wonVotes[k].ApplicationUID, err)
					}
					delete(f.wonVotes, k)
				}
			}
			f.wonVotesMutex.Unlock()
		}
	}
}

func (f *Fish) voteProcessRound(vote *types.Vote) error {
	vote.Round = f.VoteCurrentRoundGet(vote.ApplicationUID)

	app, err := f.ApplicationGet(vote.ApplicationUID)
	if err != nil {
		return log.Error("Fish: Vote Fatal: Unable to get the Application:", vote.UID, vote.ApplicationUID, err)
	}

	// Get label with the definitions
	label, err := f.LabelGet(app.LabelUID)
	if err != nil {
		return log.Error("Fish: Vote Fatal: Unable to find Label:", vote.UID, app.LabelUID, err)
	}

	for {
		startTime := time.Now()
		log.Infof("Fish: Starting Application %s election round %d", vote.ApplicationUID, vote.Round)

		// Determine answer for this round, it will try find the first possible definition to serve
		// We can't run multiple resources check at a time or together with
		// allocating application so using mutex here
		f.nodeUsageMutex.Lock()
		vote.Available = -1 // Set "nope" answer by default in case all the definitions are not fit
		for i, def := range label.Definitions {
			if f.isNodeAvailableForDefinition(def) {
				vote.Available = i
				break
			}
		}
		f.nodeUsageMutex.Unlock()

		// Create vote if it's required
		if vote.UID == uuid.Nil {
			vote.NodeUID = f.node.UID
			if err := f.VoteCreate(vote); err != nil {
				return log.Error("Fish: Unable to create vote:", vote, err)
			}
		}

		for {
			// Check all the cluster nodes are voted
			nodes, err := f.NodeActiveList()
			if err != nil {
				return log.Error("Fish: Unable to get the Node list:", err)
			}
			votes, err := f.VoteListGetApplicationRound(vote.ApplicationUID, vote.Round)
			if err != nil {
				return log.Error("Fish: Unable to get the Vote list:", err)
			}
			if len(votes) == len(nodes) {
				// Ok, all nodes are voted so let's move to election
				// Check if there's yes answers
				availableExists := false
				for _, vote := range votes {
					if vote.Available >= 0 {
						availableExists = true
						break
					}
				}

				if availableExists {
					// Check if the winner is this node
					vote, err := f.VoteGetElectionWinner(vote.ApplicationUID, vote.Round)
					if err != nil {
						return log.Error("Fish: Unable to get the election winner:", err)
					}
					if vote.NodeUID == f.node.UID {
						log.Info("Fish: I won the election for Application", vote.ApplicationUID)
						app, err := f.ApplicationGet(vote.ApplicationUID)
						if err != nil {
							return log.Error("Fish: Unable to get the Application:", vote.ApplicationUID, err)
						}
						f.wonVotesMutex.Lock()
						f.wonVotes[app.CreatedAt.UnixMicro()] = *vote
						f.wonVotesMutex.Unlock()
					} else {
						log.Infof("Fish: I lose the election for Application %s to Node %s", vote.ApplicationUID, vote.NodeUID)
					}
				}

				// Wait till the next round for ELECTION_ROUND_TIME since round start
				t := time.Now()
				toSleep := startTime.Add(ElectionRoundTime * time.Second).Sub(t)
				time.Sleep(toSleep)

				// Check if the Application changed state
				s, err := f.ApplicationStateGetByApplication(vote.ApplicationUID)
				if err != nil {
					log.Error("Fish: Unable to get the Application state:", err)
					continue
				}
				if s.Status != types.ApplicationStatusNEW {
					// The Application state was changed by some node, so we can drop the election process
					f.voteActiveRemove(vote.UID)
					return nil
				}

				// Next round seems needed
				vote.Round += 1
				vote.UID = uuid.Nil
				break
			}

			log.Debug("Fish: Some nodes didn't vote, waiting...")

			// Wait 5 sec and repeat
			time.Sleep(5 * time.Second)
		}
	}
}

func (f *Fish) isNodeAvailableForDefinition(def types.LabelDefinition) bool {
	// When node is in maintenance mode - it should not accept any Applications
	if f.maintenance {
		return false
	}

	// Is node supports the required label driver
	driver := f.DriverGet(def.Driver)
	if driver == nil {
		return false
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

func (f *Fish) executeApplication(vote types.Vote) error {
	// Check the application is executed already
	f.applicationsMutex.Lock()
	{
		for _, uid := range f.applications {
			if uid == vote.ApplicationUID {
				// Seems the application is already executing
				f.applicationsMutex.Unlock()
				return nil
			}
		}
	}
	f.applicationsMutex.Unlock()

	// Check vote have available field >= 0 means it chose the label definition
	if vote.Available < 0 {
		return fmt.Errorf("Fish: The vote for Application %s is negative: %v", vote.ApplicationUID, vote.Available)
	}

	// Locking the node resources until the app will be allocated
	f.nodeUsageMutex.Lock()

	app, err := f.ApplicationGet(vote.ApplicationUID)
	if err != nil {
		f.nodeUsageMutex.Unlock()
		return fmt.Errorf("Fish: Unable to get the Application: %v", err)
	}

	// Check current Application state
	appState, err := f.ApplicationStateGetByApplication(app.UID)
	if err != nil {
		f.nodeUsageMutex.Unlock()
		return fmt.Errorf("Fish: Unable to get the Application state: %v", err)
	}

	// Get label with the definitions
	label, err := f.LabelGet(app.LabelUID)
	if err != nil {
		f.nodeUsageMutex.Unlock()
		return fmt.Errorf("Fish: Unable to find Label %s: %v", app.LabelUID, err)
	}

	// Extract the vote won Label Definition
	if len(label.Definitions) <= vote.Available {
		f.nodeUsageMutex.Unlock()
		return fmt.Errorf("Fish: ERROR: The voted Definition not exists in the Label %s: %v (App: %s)", app.LabelUID, vote.Available, app.UID)
	}
	labelDef := label.Definitions[vote.Available]

	// The already running applications will not consume the additional resources
	if appState.Status == types.ApplicationStatusNEW {
		// In case there is multiple Applications won the election process on the same node it could
		// just have not enough resources, so skip it for now to allow the other Nodes to try again.
		if !f.isNodeAvailableForDefinition(labelDef) {
			log.Warn("Fish: Not enough resources to execute the Application", app.UID)
			f.nodeUsageMutex.Unlock()
			return nil
		}
	}

	// Locate the required driver
	driver := f.DriverGet(labelDef.Driver)
	if driver == nil {
		f.nodeUsageMutex.Unlock()
		return fmt.Errorf("Fish: Unable to locate driver for the Application %s: %s", app.UID, labelDef.Driver)
	}

	// If the driver is not using the remote resources - we need to increase the counter
	if !driver.IsRemote() {
		f.nodeUsage.Add(labelDef.Resources)
	}

	// Unlocking the node resources to allow the other Applications allocation
	f.nodeUsageMutex.Unlock()

	// Adding the application to list
	f.applicationsMutex.Lock()
	f.applications = append(f.applications, app.UID)
	f.applicationsMutex.Unlock()

	// The main application processing is executed on background because allocation could take a
	// while, after that the bg process will wait for application state change
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
		res := &types.Resource{
			ApplicationUID: app.UID,
			NodeUID:        f.node.UID,
			Metadata:       util.UnparsedJson(mergedMetadata),
		}
		if appState.Status == types.ApplicationStatusALLOCATED {
			res, err = f.ResourceGetByApplication(app.UID)
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
			log.Infof("Fish: Allocate the Application %s resource using driver: %s", app.UID, driver.Name())
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
				res.DefinitionIndex = vote.Available
				res.Authentication = drvRes.Authentication
				err := f.ResourceCreate(res)
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
			if !f.running {
				log.Info("Fish: Stopping the Application execution:", app.UID)
				return
			}
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
						deallocateRetry += 1
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
				if err := f.ResourceDelete(res.UID); err != nil {
					log.Error("Fish: Unable to delete Resource for Application:", app.UID, err)
				}
				f.ApplicationStateCreate(appState)
			} else {
				time.Sleep(5 * time.Second)
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

func (f *Fish) executeApplicationTasks(drv drivers.ResourceDriver, def *types.LabelDefinition, res *types.Resource, appStatus types.ApplicationStatus) error {
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
			task.Result = util.UnparsedJson(`{"error":"task not available in driver"}`)
		} else {
			// Executing the task
			t.SetInfo(&task, def, res)
			result, err := t.Execute()
			if err != nil {
				// We're not crashing here because even with error task could have a result
				log.Error("Fish: Error happened during executing the task:", task.UID, err)
			}
			task.Result = util.UnparsedJson(result)
		}
		if err := f.ApplicationTaskSave(&task); err != nil {
			log.Error("Fish: Error during update the task with result:", task.UID, err)
		}
	}

	return nil
}

func (f *Fish) removeFromExecutingApplincations(appUid types.ApplicationUID) {
	for i, uid := range f.applications {
		if uid != appUid {
			continue
		}
		f.applications[i] = f.applications[len(f.applications)-1]
		f.applications = f.applications[:len(f.applications)-1]
		break
	}
}

func (f *Fish) voteActive(appUid types.ApplicationUID) bool {
	f.activeVotesMutex.Lock()
	defer f.activeVotesMutex.Unlock()

	for _, vote := range f.activeVotes {
		if vote.ApplicationUID == appUid {
			return true
		}
	}
	return false
}

func (f *Fish) voteActiveRemove(voteUid types.VoteUID) {
	f.activeVotesMutex.Lock()
	defer f.activeVotesMutex.Unlock()
	av := f.activeVotes

	for i, v := range f.activeVotes {
		if v.UID != voteUid {
			continue
		}
		av[i] = av[len(av)-1]
		f.activeVotes = av[:len(av)-1]
		break
	}
}

// Set/unset the maintenance mode which will not allow to accept the additional Applications
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

// Tells node it need to execute graceful shutdown operation
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

// Set of how much time to wait before executing the node shutdown operation
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
					delayTickerReport := time.NewTicker(30 * time.Second)
					defer delayTickerReport.Stop()
					delayTimer = time.NewTimer(f.shutdownDelay)
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
