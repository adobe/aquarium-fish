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
	"math/rand"
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

const ELECTION_ROUND_TIME = 30

type Fish struct {
	db   *gorm.DB
	cfg  *Config
	node *types.Node

	// Signal to stop the fish
	Quit chan os.Signal

	running         bool
	maintenance     bool
	shutdown        bool
	shutdown_cancel chan bool
	shutdown_delay  time.Duration

	active_votes_mutex sync.Mutex
	active_votes       []*types.Vote

	// Stores the currently executing Applications
	applications_mutex sync.Mutex
	applications       []types.ApplicationUID

	// Used to temporarly store the won Votes by Application create time
	won_votes_mutex sync.Mutex
	won_votes       map[int64]types.Vote

	// Stores the current usage of the node resources
	node_usage_mutex sync.Mutex // Is needed to protect node resources from concurrent allocations
	node_usage       types.Resources
}

func New(db *gorm.DB, cfg *Config) (*Fish, error) {
	// Init rand generator
	rand.Seed(time.Now().UnixNano())

	f := &Fish{db: db, cfg: cfg}
	if err := f.Init(); err != nil {
		return nil, err
	}

	return f, nil
}

func (f *Fish) Init() error {
	f.shutdown_cancel = make(chan bool)
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
	f.won_votes = make(map[int64]types.Vote, 5)

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
	create_node := false
	node, err := f.NodeGet(f.cfg.NodeName)
	if err != nil {
		log.Info("Fish: Create new node:", f.cfg.NodeName, f.cfg.NodeLocation)
		create_node = true

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

	cert_path := f.cfg.TLSCrt
	if !filepath.IsAbs(cert_path) {
		cert_path = filepath.Join(f.cfg.Directory, cert_path)
	}
	if err := node.Init(f.cfg.NodeAddress, cert_path); err != nil {
		return fmt.Errorf("Fish: Unable to init node: %v", err)
	}

	f.node = node
	if create_node {
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
			app_state := &types.ApplicationState{ApplicationUID: res.ApplicationUID, Status: types.ApplicationStatusERROR,
				Description: "Found not cleaned up resource",
			}
			f.ApplicationStateCreate(app_state)
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

func (f *Fish) checkNewApplicationProcess() error {
	check_ticker := time.NewTicker(5 * time.Second)
	for {
		if !f.running {
			break
		}
		select {
		case <-check_ticker.C:
			// Check new apps available for processing
			new_apps, err := f.ApplicationListGetStatusNew()
			if err != nil {
				log.Error("Fish: Unable to get NEW ApplicationState list:", err)
				continue
			}
			for _, app := range new_apps {
				// Check if Vote is already here
				if f.voteActive(app.UID) {
					continue
				}
				log.Info("Fish: NEW Application with no vote:", app.UID, app.CreatedAt)

				// Vote not exists in the active votes - running the process
				f.active_votes_mutex.Lock()
				{
					// Check if it's already exist in the DB (if node was restarted during voting)
					vote, _ := f.VoteGetNodeApplication(f.node.UID, app.UID)

					// Ensure the app & node is set in the vote
					vote.ApplicationUID = app.UID
					vote.NodeUID = f.node.UID

					f.active_votes = append(f.active_votes, vote)
					go f.voteProcessRound(vote)
				}
				f.active_votes_mutex.Unlock()
			}

			// Check the Applications ready to be allocated
			// It's needed to be single-threaded to have some order in allocation - FIFO principle,
			// who requested first should be processed first.
			f.won_votes_mutex.Lock()
			{
				// We need to sort the won_votes by key which is time they was created
				keys := make([]int64, 0, len(f.won_votes))
				for k := range f.won_votes {
					keys = append(keys, k)
				}
				sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

				for _, k := range keys {
					if err := f.executeApplication(f.won_votes[k]); err != nil {
						log.Errorf("Fish: Can't execute Application %s: %v", f.won_votes[k].ApplicationUID, err)
					}
					delete(f.won_votes, k)
				}
			}
			f.won_votes_mutex.Unlock()
		}
	}
	return nil
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
		start_time := time.Now()
		log.Infof("Fish: Starting Application %s election round %d", vote.ApplicationUID, vote.Round)

		// Determine answer for this round, it will try find the first possible definition to serve
		// We can't run multiple resources check at a time or together with
		// allocating application so using mutex here
		f.node_usage_mutex.Lock()
		vote.Available = -1 // Set "nope" answer by default in case all the definitions are not fit
		for i, def := range label.Definitions {
			if f.isNodeAvailableForDefinition(def) {
				vote.Available = i
				break
			}
		}
		f.node_usage_mutex.Unlock()

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
				available_exists := false
				for _, vote := range votes {
					if vote.Available >= 0 {
						available_exists = true
						break
					}
				}

				if available_exists {
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
						f.won_votes_mutex.Lock()
						f.won_votes[app.CreatedAt.UnixMicro()] = *vote
						f.won_votes_mutex.Unlock()
					} else {
						log.Infof("Fish: I lose the election for Application %s to Node %s", vote.ApplicationUID, vote.NodeUID)
					}
				}

				// Wait till the next round for ELECTION_ROUND_TIME since round start
				t := time.Now()
				to_sleep := start_time.Add(ELECTION_ROUND_TIME * time.Second).Sub(t)
				time.Sleep(to_sleep)

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
	if def.Resources.NodeFilter != nil && len(def.Resources.NodeFilter) > 0 {
		needed_idents := def.Resources.NodeFilter
		current_idents := f.cfg.NodeIdentifiers
		for _, needed := range needed_idents {
			found := false
			for _, value := range current_idents {
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
	node_usage := f.node_usage
	if capacity := driver.AvailableCapacity(node_usage, def); capacity < 1 {
		return false
	}

	return true
}

func (f *Fish) executeApplication(vote types.Vote) error {
	// Check the application is executed already
	f.applications_mutex.Lock()
	{
		for _, uid := range f.applications {
			if uid == vote.ApplicationUID {
				// Seems the application is already executing
				f.applications_mutex.Unlock()
				return nil
			}
		}
	}
	f.applications_mutex.Unlock()

	// Check vote have available field >= 0 means it chose the label definition
	if vote.Available < 0 {
		return fmt.Errorf("Fish: The vote for Application %s is negative: %v", vote.ApplicationUID, vote.Available)
	}

	// Locking the node resources until the app will be allocated
	f.node_usage_mutex.Lock()

	app, err := f.ApplicationGet(vote.ApplicationUID)
	if err != nil {
		f.node_usage_mutex.Unlock()
		return fmt.Errorf("Fish: Unable to get the Application: %v", err)
	}

	// Check current Application state
	app_state, err := f.ApplicationStateGetByApplication(app.UID)
	if err != nil {
		f.node_usage_mutex.Unlock()
		return fmt.Errorf("Fish: Unable to get the Application state: %v", err)
	}

	// Get label with the definitions
	label, err := f.LabelGet(app.LabelUID)
	if err != nil {
		f.node_usage_mutex.Unlock()
		return fmt.Errorf("Fish: Unable to find Label %s: %v", app.LabelUID, err)
	}

	// Extract the vote won Label Definition
	if len(label.Definitions) <= vote.Available {
		f.node_usage_mutex.Unlock()
		return fmt.Errorf("Fish: ERROR: The voted Definition not exists in the Label %s: %v (App: %s)", app.LabelUID, vote.Available, app.UID)
	}
	label_def := label.Definitions[vote.Available]

	// The already running applications will not consume the additional resources
	if app_state.Status == types.ApplicationStatusNEW {
		// In case there is multiple Applications won the election process on the same node it could
		// just have not enough resources, so skip it for now to allow the other Nodes to try again.
		if !f.isNodeAvailableForDefinition(label_def) {
			log.Warn("Fish: Not enough resources to execute the Application", app.UID)
			f.node_usage_mutex.Unlock()
			return nil
		}
	}

	// Locate the required driver
	driver := f.DriverGet(label_def.Driver)
	if driver == nil {
		f.node_usage_mutex.Unlock()
		return fmt.Errorf("Fish: Unable to locate driver for the Application %s: %s", app.UID, label_def.Driver)
	}

	// If the driver is not using the remote resources - we need to increase the counter
	if !driver.IsRemote() {
		f.node_usage.Add(label_def.Resources)
	}

	// Unlocking the node resources to allow the other Applications allocation
	f.node_usage_mutex.Unlock()

	// Adding the application to list
	f.applications_mutex.Lock()
	f.applications = append(f.applications, app.UID)
	f.applications_mutex.Unlock()

	// The main application processing is executed on background because allocation could take a
	// while, after that the bg process will wait for application state change
	go func() {
		log.Info("Fish: Start executing Application", app.UID, app_state.Status)

		if app_state.Status == types.ApplicationStatusNEW {
			// Set Application state as ELECTED
			app_state = &types.ApplicationState{ApplicationUID: app.UID, Status: types.ApplicationStatusELECTED,
				Description: "Elected node: " + f.node.Name,
			}
			err := f.ApplicationStateCreate(app_state)
			if err != nil {
				log.Error("Fish: Unable to set Application state:", app.UID, err)
				f.applications_mutex.Lock()
				f.removeFromExecutingApplincations(app.UID)
				f.applications_mutex.Unlock()
				return
			}
		}

		// Merge application and label metadata, in this exact order
		var merged_metadata []byte
		var metadata map[string]any
		if err := json.Unmarshal([]byte(app.Metadata), &metadata); err != nil {
			log.Error("Fish: Unable to parse the Application metadata:", app.UID, err)
			app_state = &types.ApplicationState{ApplicationUID: app.UID, Status: types.ApplicationStatusERROR,
				Description: fmt.Sprint("Unable to parse the app metadata:", err),
			}
			f.ApplicationStateCreate(app_state)
		}
		if err := json.Unmarshal([]byte(label.Metadata), &metadata); err != nil {
			log.Error("Fish: Unable to parse the Label metadata:", label.UID, err)
			app_state = &types.ApplicationState{ApplicationUID: app.UID, Status: types.ApplicationStatusERROR,
				Description: fmt.Sprint("Unable to parse the label metadata:", err),
			}
			f.ApplicationStateCreate(app_state)
		}
		if merged_metadata, err = json.Marshal(metadata); err != nil {
			log.Error("Fish: Unable to merge metadata:", label.UID, err)
			app_state = &types.ApplicationState{ApplicationUID: app.UID, Status: types.ApplicationStatusERROR,
				Description: fmt.Sprint("Unable to merge metadata:", err),
			}
			f.ApplicationStateCreate(app_state)
		}

		// Get or create the new resource object
		res := &types.Resource{
			ApplicationUID: app.UID,
			NodeUID:        f.node.UID,
			Metadata:       util.UnparsedJson(merged_metadata),
		}
		if app_state.Status == types.ApplicationStatusALLOCATED {
			res, err = f.ResourceGetByApplication(app.UID)
			if err != nil {
				log.Error("Fish: Unable to get the allocated Resource for Application:", app.UID, err)
				app_state = &types.ApplicationState{ApplicationUID: app.UID, Status: types.ApplicationStatusERROR,
					Description: fmt.Sprint("Unable to find the allocated resource:", err),
				}
				f.ApplicationStateCreate(app_state)
			}
		}

		// Allocate the resource
		if app_state.Status == types.ApplicationStatusELECTED {
			// Run the allocation
			log.Info("Fish: Allocate the resource using the driver", driver.Name())
			drv_res, err := driver.Allocate(label_def, metadata)
			if err != nil {
				log.Error("Fish: Unable to allocate resource for the Application:", app.UID, err)
				app_state = &types.ApplicationState{ApplicationUID: app.UID, Status: types.ApplicationStatusERROR,
					Description: fmt.Sprint("Driver allocate resource error:", err),
				}
			} else {
				res.Identifier = drv_res.Identifier
				res.HwAddr = drv_res.HwAddr
				res.IpAddr = drv_res.IpAddr
				res.LabelUID = label.UID
				res.DefinitionIndex = vote.Available
				res.Authentication = drv_res.Authentication
				err := f.ResourceCreate(res)
				if err != nil {
					log.Error("Fish: Unable to store Resource for Application:", app.UID, err)
				}
				app_state = &types.ApplicationState{ApplicationUID: app.UID, Status: types.ApplicationStatusALLOCATED,
					Description: fmt.Sprint("Driver allocated the resource"),
				}
			}
			f.ApplicationStateCreate(app_state)
		}

		// Getting the resource lifetime to know how much time it will live
		resource_lifetime, err := time.ParseDuration(label_def.Resources.Lifetime)
		if label_def.Resources.Lifetime != "" && err != nil {
			log.Error("Fish: Can't parse the Lifetime from Label Definition:", label.UID, res.DefinitionIndex)
		}
		if err != nil {
			// Try to get default value from fish config
			resource_lifetime, err = time.ParseDuration(f.cfg.DefaultResourceLifetime)
			if err != nil {
				// Not an error - in worst case the resource will just sit there but at least will
				// not ruin the workload execution
				log.Warn("Fish: Default Resource Lifetime is not set in fish config")
			}
		}
		resource_timeout := res.CreatedAt.Add(resource_lifetime)
		if app_state.Status == types.ApplicationStatusALLOCATED {
			if resource_lifetime > 0 {
				log.Infof("Fish: Resource of Application %s will be deallocated by timeout in %s (%s)", app.UID, resource_lifetime, resource_timeout)
			} else {
				log.Warn("Fish: Resource have no lifetime set and will live until deallocated by user:", app.UID)
			}
		}

		// Run the loop to wait for deallocate request
		var deallocate_retry uint8 = 1
		for app_state.Status == types.ApplicationStatusALLOCATED {
			if !f.running {
				log.Info("Fish: Stopping the Application execution:", app.UID)
				return
			}
			app_state, err = f.ApplicationStateGetByApplication(app.UID)
			if err != nil {
				log.Error("Fish: Unable to get Status for Application:", app.UID, err)
			}

			// Check if it's life timeout for the resource
			if resource_lifetime > 0 {
				// The time limit is set - so let's use resource create time and find out timeout
				if resource_timeout.Before(time.Now()) {
					// Seems the timeout has come, so fish asks for application deallocate
					app_state = &types.ApplicationState{ApplicationUID: app.UID, Status: types.ApplicationStatusDEALLOCATE,
						Description: fmt.Sprint("Resource lifetime timeout reached:", resource_lifetime),
					}
					f.ApplicationStateCreate(app_state)
				}
			}

			// Execute the existing ApplicationTasks. It will be executed during ALLOCATED or prior
			// to executing deallocation by DEALLOCATE & RECALLED which right now is useful for
			// `snapshot` and `image` tasks.
			f.executeApplicationTasks(driver, &label_def, res, app_state.Status)

			if app_state.Status == types.ApplicationStatusDEALLOCATE || app_state.Status == types.ApplicationStatusRECALLED {
				log.Info("Fish: Running Deallocate of the Application:", app.UID)
				// Deallocating and destroy the resource
				if err := driver.Deallocate(res); err != nil {
					log.Errorf("Fish: Unable to deallocate the Resource of Application: %s (try: %d): %v", app.UID, deallocate_retry, err)
					// Let's retry to deallocate the resource 10 times before give up
					if deallocate_retry <= 10 {
						deallocate_retry += 1
						time.Sleep(10 * time.Second)
						continue
					}
					app_state = &types.ApplicationState{ApplicationUID: app.UID, Status: types.ApplicationStatusERROR,
						Description: fmt.Sprint("Driver deallocate resource error:", err),
					}
				} else {
					log.Info("Fish: Successful deallocation of the Application:", app.UID)
					app_state = &types.ApplicationState{ApplicationUID: app.UID, Status: types.ApplicationStatusDEALLOCATED,
						Description: fmt.Sprint("Driver deallocated the resource"),
					}
				}
				// Destroying the resource anyway to not bloat the table - otherwise it will stuck there and
				// will block the access to IP of the other VM's that will reuse this IP
				if err := f.ResourceDelete(res.UID); err != nil {
					log.Error("Fish: Unable to delete Resource for Application:", app.UID, err)
				}
				f.ApplicationStateCreate(app_state)
			} else {
				time.Sleep(5 * time.Second)
			}
		}

		f.applications_mutex.Lock()
		{
			// Decrease the amout of running local apps
			if !driver.IsRemote() {
				f.node_usage_mutex.Lock()
				f.node_usage.Subtract(label_def.Resources)
				f.node_usage_mutex.Unlock()
			}

			// Clean the executing application
			f.removeFromExecutingApplincations(app.UID)
		}
		f.applications_mutex.Unlock()

		log.Info("Fish: Done executing Application", app.UID, app_state.Status)
	}()

	return nil
}

func (f *Fish) executeApplicationTasks(drv drivers.ResourceDriver, def *types.LabelDefinition, res *types.Resource, app_status types.ApplicationStatus) error {
	// Execute the associated ApplicationTasks if there is some
	tasks, err := f.ApplicationTaskListByApplicationAndWhen(res.ApplicationUID, app_status)
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
			task.Result = util.UnparsedJson(`{"error":"task not availble in driver"}`)
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

func (f *Fish) removeFromExecutingApplincations(app_uid types.ApplicationUID) {
	for i, uid := range f.applications {
		if uid != app_uid {
			continue
		}
		f.applications[i] = f.applications[len(f.applications)-1]
		f.applications = f.applications[:len(f.applications)-1]
		break
	}
}

func (f *Fish) voteActive(app_uid types.ApplicationUID) bool {
	f.active_votes_mutex.Lock()
	defer f.active_votes_mutex.Unlock()

	for _, vote := range f.active_votes {
		if vote.ApplicationUID == app_uid {
			return true
		}
	}
	return false
}

func (f *Fish) voteActiveRemove(vote_uid types.VoteUID) {
	f.active_votes_mutex.Lock()
	defer f.active_votes_mutex.Unlock()
	av := f.active_votes

	for i, v := range f.active_votes {
		if v.UID != vote_uid {
			continue
		}
		av[i] = av[len(av)-1]
		f.active_votes = av[:len(av)-1]
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
			f.shutdown_cancel <- true
		}
	}

	f.shutdown = value
}

// Set of how much time to wait before executing the node shutdown operation
func (f *Fish) ShutdownDelaySet(delay time.Duration) {
	if f.shutdown_delay != delay {
		log.Info("Fish: Shutdown delay is set to:", delay)
	}

	f.shutdown_delay = delay
}

func (f *Fish) activateShutdown() {
	log.Infof("Fish: Enabled shutdown mode with maintenance: %v, delay: %v", f.maintenance, f.shutdown_delay)

	wait_apps := make(chan bool, 1)

	// Running the main shutdown routine
	go func() {
		fire_shutdown := make(chan bool, 1)
		delay_ticker_report := &time.Ticker{}
		delay_timer := &time.Timer{}
		var delay_end_time time.Time

		for {
			select {
			case <-f.shutdown_cancel:
				return
			case <-wait_apps:
				// Maintenance mode: All the apps are completed so it's safe to shutdown
				log.Debug("Fish: Shutdown: apps execution completed")
				// If the delay is set, then running timer to execute shutdown with delay
				if f.shutdown_delay > 0 {
					delay_end_time = time.Now().Add(f.shutdown_delay)
					delay_ticker_report := time.NewTicker(30 * time.Second)
					defer delay_ticker_report.Stop()
					delay_timer = time.NewTimer(f.shutdown_delay)
					defer delay_timer.Stop()
				} else {
					// No delay is needed, so shutdown now
					fire_shutdown <- true
				}
			case <-delay_ticker_report.C:
				log.Infof("Fish: Shutdown: countdown: T-%v", delay_end_time.Sub(time.Now()))
			case <-delay_timer.C:
				// Delay time has passed, triggering shutdown
				fire_shutdown <- true
			case <-fire_shutdown:
				log.Info("Fish: Shutdown sends quit signal to Fish")
				f.Quit <- syscall.SIGQUIT
			}
		}
	}()

	if f.maintenance {
		// Running wait for unfinished apps go routine
		go func() {
			ticker_check := time.NewTicker(2 * time.Second)
			defer ticker_check.Stop()
			ticker_report := time.NewTicker(30 * time.Second)
			defer ticker_report.Stop()

			for {
				select {
				case <-f.shutdown_cancel:
					return
				case <-ticker_check.C:
					// Need to make sure we're not executing any workload
					log.Debug("Fish: Shutdown: checking apps execution:", len(f.applications))
					if len(f.applications) == 0 {
						wait_apps <- true
						return
					}
				case <-ticker_report.C:
					log.Info("Fish: Shutdown: waiting for running Applications:", len(f.applications))
				}
			}
		}()
	} else {
		// Sending signal since no need to wait for the apps
		wait_apps <- true
	}
}
