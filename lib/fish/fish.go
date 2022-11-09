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
	"log"
	"math/rand"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mostlygeek/arp"
	"gorm.io/gorm"

	"github.com/adobe/aquarium-fish/lib/drivers"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
	"github.com/adobe/aquarium-fish/lib/util"
)

const ELECTION_ROUND_TIME = 30

type Fish struct {
	db   *gorm.DB
	cfg  *Config
	node *types.Node

	running bool

	active_votes_mutex sync.Mutex
	active_votes       []*types.Vote

	// Stores the currently executing Applications
	applications_mutex sync.Mutex
	applications       []types.ApplicationUID

	// Used to temporarly store the won Applications
	won_apps_mutex sync.Mutex
	won_apps       map[int64]types.ApplicationUID

	// Stores the current usage of the node resources
	node_usage_mutex sync.Mutex // Is needed to protect node resources from concurrent allocations
	node_usage       drivers.Resources
}

func New(db *gorm.DB, cfg *Config) (*Fish, error) {
	// Init rand generator
	rand.Seed(time.Now().UnixNano())

	f := &Fish{db: db, cfg: cfg}
	if err := f.Init(); err != nil {
		return nil, err
	}

	if err := f.DriversSet(); err != nil {
		return nil, err
	}
	if errs := f.DriversPrepare(cfg.Drivers); errs != nil {
		log.Println("Fish: Unable to prepare some resource drivers", errs)
	}

	return f, nil
}

func (f *Fish) Init() error {
	if err := f.db.AutoMigrate(
		&types.User{},
		&types.Node{},
		&types.Label{},
		&types.Application{},
		&types.ApplicationState{},
		&types.Resource{},
		&types.Vote{},
		&types.Location{},
		&types.ServiceMapping{},
	); err != nil {
		return fmt.Errorf("Fish: Unable to apply DB schema: %v", err)
	}

	// Init variables
	f.won_apps = make(map[int64]types.ApplicationUID, 5)

	// Create admin user and ignore errors if it's existing
	_, err := f.UserGet("admin")
	if err == gorm.ErrRecordNotFound {
		if pass, _ := f.UserNew("admin", ""); pass != "" {
			// Print pass of newly created admin user to stderr
			println("Admin user pass:", pass)
		}
	} else if err != nil {
		return fmt.Errorf("Fish: Unable to create admin: %v", err)
	}

	// Init node
	create_node := false
	node, err := f.NodeGet(f.cfg.NodeName)
	if err != nil {
		log.Println("Fish: Create new node:", f.cfg.NodeName, f.cfg.NodeLocation)
		create_node = true

		node = &types.Node{
			Name: f.cfg.NodeName,
		}
		if f.cfg.NodeLocation != "" {
			loc, err := f.LocationGetByName(f.cfg.NodeLocation)
			if err != nil {
				log.Println("Fish: Creating new location", f.cfg.NodeLocation)
				loc.Name = f.cfg.NodeLocation
				loc.Description = fmt.Sprintf("Created automatically during node '%s' startup", f.cfg.NodeName)
				if f.LocationCreate(loc) != nil {
					return fmt.Errorf("Fish: Unable to create new location")
				}
			}
			node.LocationName = loc.Name
		}
	} else {
		log.Println("Fish: Use existing node:", node.Name, node.LocationName)
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

	// Fish is running now
	f.running = true

	// Continue to execute the assigned applications
	resources, err := f.ResourceListNode(f.node.UID)
	if err != nil {
		log.Println("Fish: Unable to get the node resources:", err)
		return err
	}
	for _, res := range resources {
		if f.ApplicationIsAllocated(res.ApplicationUID) == nil {
			log.Println("Fish: Found allocated resource to serve:", res.UID)
			if err := f.executeApplication(res.ApplicationUID); err != nil {
				log.Printf("Fish: Can't execute Application %s: %v\n", res.ApplicationUID, err)
			}
		} else {
			log.Println("Fish: WARN: Found not allocated Resource of Application, cleaning up:", res.ApplicationUID)
			if err := f.ResourceDelete(res.UID); err != nil {
				log.Println("Fish: Unable to delete Resource of Application:", res.ApplicationUID, err)
			}
			app_state := &types.ApplicationState{ApplicationUID: res.ApplicationUID, Status: types.ApplicationStateStatusERROR,
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
				log.Println("Fish: Unable to get NEW ApplicationState list:", err)
				continue
			}
			for _, app := range new_apps {
				// Check if Vote is already here
				if f.voteActive(app.UID) {
					continue
				}
				log.Println("Fish: NEW Application with no vote:", app.UID, app.CreatedAt)

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
			f.won_apps_mutex.Lock()
			{
				// We need to sort the won_apps by key which is time they was created
				keys := make([]int64, 0, len(f.won_apps))
				for k := range f.won_apps {
					keys = append(keys, k)
				}
				sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

				for _, k := range keys {
					if err := f.executeApplication(f.won_apps[k]); err != nil {
						log.Printf("Fish: Can't execute Application %s: %v\n", f.won_apps[k], err)
					}
					delete(f.won_apps, k)
				}
			}
			f.won_apps_mutex.Unlock()
		}
	}
	return nil
}

func (f *Fish) voteProcessRound(vote *types.Vote) {
	vote.Round = f.VoteCurrentRoundGet(vote.ApplicationUID)

	for {
		start_time := time.Now()
		log.Printf("Fish: Starting Application %s election round %d\n", vote.ApplicationUID, vote.Round)

		// Determine answer for this round
		// We can't run multiple resources check at a time or together with
		// allocating application so using mutex here
		f.node_usage_mutex.Lock()
		vote.Available = f.isNodeAvailableForApplication(vote.ApplicationUID)
		f.node_usage_mutex.Unlock()

		// Create vote if it's required
		if vote.UID == uuid.Nil {
			vote.NodeUID = f.node.UID
			if err := f.VoteCreate(vote); err != nil {
				log.Println("Fish: Unable to create vote:", vote, err)
				return
			}
		}

		for {
			// Check all the cluster nodes are voted
			nodes, err := f.NodeActiveList()
			if err != nil {
				log.Println("Fish: Unable to get the Node list:", err)
				return
			}
			votes, err := f.VoteListGetApplicationRound(vote.ApplicationUID, vote.Round)
			if err != nil {
				log.Println("Fish: Unable to get the Vote list:", err)
				return
			}
			if len(votes) == len(nodes) {
				// Ok, all nodes are voted so let's move to election
				// Check if there's yes answers
				available_exists := false
				for _, vote := range votes {
					if vote.Available {
						available_exists = true
						break
					}
				}

				if available_exists {
					// Check if the winner is this node
					vote, err := f.VoteGetElectionWinner(vote.ApplicationUID, vote.Round)
					if err != nil {
						log.Println("Fish: Unable to get the election winner:", err)
						return
					}
					if vote.NodeUID == f.node.UID {
						log.Println("Fish: I won the election for Application", vote.ApplicationUID)
						app, err := f.ApplicationGet(vote.ApplicationUID)
						if err != nil {
							log.Println("Fish: Unable to get the Application:", vote.ApplicationUID, err)
							return
						}
						f.won_apps_mutex.Lock()
						f.won_apps[app.CreatedAt.UnixMicro()] = vote.ApplicationUID
						f.won_apps_mutex.Unlock()
					} else {
						log.Println("Fish: I lose the election for Application", vote.ApplicationUID)
					}
				}

				// Wait till the next round for ELECTION_ROUND_TIME since round start
				t := time.Now()
				to_sleep := start_time.Add(ELECTION_ROUND_TIME * time.Second).Sub(t)
				time.Sleep(to_sleep)

				// Check if the Application changed state
				s, err := f.ApplicationStateGetByApplication(vote.ApplicationUID)
				if err != nil {
					log.Println("Fish: Unable to get the Application state:", err)
					continue
				}
				if s.Status != types.ApplicationStateStatusNEW {
					// The Application state was changed by some node, so we can drop the election process
					f.voteActiveRemove(vote.UID)
					return
				}

				// Next round seems needed
				vote.Round += 1
				vote.UID = uuid.Nil
				break
			}

			log.Println("Fish: Some nodes didn't vote, waiting...")

			// Wait 5 sec and repeat
			time.Sleep(5 * time.Second)
		}
	}
}

func (f *Fish) isNodeAvailableForApplication(app_uid types.ApplicationUID) bool {
	app, err := f.ApplicationGet(app_uid)
	if err != nil {
		log.Println("Fish: Unable to find Application", app_uid, err)
		return false
	}
	label, err := f.LabelGet(app.LabelUID)
	if err != nil {
		log.Println("Fish: Unable to find Label", app.LabelUID)
		return false
	}

	// Is node supports the required label driver
	driver := f.DriverGet(label.Driver)
	if driver == nil {
		return false
	}

	// Check with the driver if it's possible to allocate the Application resource
	node_usage := f.node_usage
	if capacity := driver.AvailableCapacity(node_usage, string(label.Definition)); capacity < 1 {
		log.Printf("Fish: Not enough Driver '%s' capacity to serve the Application: %s", driver.Name(), app_uid)
		return false
	}

	return true
}

func (f *Fish) executeApplication(app_uid types.ApplicationUID) error {
	f.applications_mutex.Lock()
	{
		// Check the application is executed already
		for _, uid := range f.applications {
			if uid == app_uid {
				// Seems the application is already executing
				f.applications_mutex.Unlock()
				return nil
			}
		}
	}
	f.applications_mutex.Unlock()

	// Locking the node resources until the app will be allocated
	f.node_usage_mutex.Lock()

	app, err := f.ApplicationGet(app_uid)
	if err != nil {
		f.node_usage_mutex.Unlock()
		return fmt.Errorf("Fish: Unable to get the Application: %v", err)
	}

	// In case there is multiple Applications won the election process on the same node it could
	// just have not enough resources, so skip it for now to allow the other Nodes to try again.
	if !f.isNodeAvailableForApplication(app_uid) {
		log.Println("Fish: Not enough resources to execute the Application", app_uid)
		f.node_usage_mutex.Unlock()
		return nil
	}

	// Check current Application state
	app_state, err := f.ApplicationStateGetByApplication(app.UID)
	if err != nil {
		f.node_usage_mutex.Unlock()
		return fmt.Errorf("Fish: Unable to get the Application state: %v", err)
	}

	// Get label with the definition
	label, err := f.LabelGet(app.LabelUID)
	if err != nil {
		f.node_usage_mutex.Unlock()
		return fmt.Errorf("Fish: Unable to find Label %s: %v", app.LabelUID, err)
	}

	// Locate the required driver
	driver := f.DriverGet(label.Driver)
	if driver == nil {
		f.node_usage_mutex.Unlock()
		return fmt.Errorf("Fish: Unable to locate driver for the Application %v", app.UID)
	}

	// If the driver is not using the remote resources - we need to increase the counter
	if !driver.IsRemote() {
		f.node_usage.Add(driver.DefinitionResources(string(label.Definition)))
	}

	// Unlocking the node resources to allow the other Applications allocation
	f.node_usage_mutex.Unlock()

	// Adding the application to list
	f.applications_mutex.Lock()
	f.applications = append(f.applications, app_uid)
	f.applications_mutex.Unlock()

	// The main application processing is executed on background because allocation could take a
	// while, after that the bg process will wait for application state change
	go func() {
		log.Println("Fish: Start executing Application", app.UID, app_state.Status)

		if app_state.Status == types.ApplicationStateStatusNEW {
			// Set Application state as ELECTED
			app_state = &types.ApplicationState{ApplicationUID: app.UID, Status: types.ApplicationStateStatusELECTED,
				Description: "Elected node: " + f.node.Name,
			}
			err := f.ApplicationStateCreate(app_state)
			if err != nil {
				log.Println("Fish: Unable to set Application state:", app.UID, err)
				f.applications_mutex.Lock()
				f.removeFromExecutingApplincations(app_uid)
				f.applications_mutex.Unlock()
				return
			}
		}

		// Merge application and label metadata, in this exact order
		var merged_metadata []byte
		var metadata map[string]interface{}
		if err := json.Unmarshal([]byte(app.Metadata), &metadata); err != nil {
			log.Println("Fish: Unable to parse the app metadata:", app.UID, err)
			app_state = &types.ApplicationState{ApplicationUID: app.UID, Status: types.ApplicationStateStatusERROR,
				Description: fmt.Sprintf("Unable to parse the app metadata: %s", err),
			}
			f.ApplicationStateCreate(app_state)
		}
		if err := json.Unmarshal([]byte(label.Metadata), &metadata); err != nil {
			log.Println("Fish: Unable to parse the Label metadata:", label.UID, err)
			app_state = &types.ApplicationState{ApplicationUID: app.UID, Status: types.ApplicationStateStatusERROR,
				Description: fmt.Sprintf("Unable to parse the label metadata: %s", err),
			}
			f.ApplicationStateCreate(app_state)
		}
		if merged_metadata, err = json.Marshal(metadata); err != nil {
			log.Println("Fish: Unable to merge metadata:", label.UID, err)
			app_state = &types.ApplicationState{ApplicationUID: app.UID, Status: types.ApplicationStateStatusERROR,
				Description: fmt.Sprintf("Unable to merge metadata: %s", err),
			}
			f.ApplicationStateCreate(app_state)
		}

		// Get or create the new resource object
		res := &types.Resource{
			ApplicationUID: app.UID,
			NodeUID:        f.node.UID,
			Metadata:       util.UnparsedJson(merged_metadata),
		}
		if app_state.Status == types.ApplicationStateStatusALLOCATED {
			res, err = f.ResourceGetByApplication(app.UID)
			if err != nil {
				log.Println("Fish: Unable to get the allocated resource for Application:", app.UID, err)
				app_state = &types.ApplicationState{ApplicationUID: app.UID, Status: types.ApplicationStateStatusERROR,
					Description: fmt.Sprintf("Unable to find the allocated resource: %s", err),
				}
				f.ApplicationStateCreate(app_state)
			}
		}

		// Allocate the resource
		if app_state.Status == types.ApplicationStateStatusELECTED {
			// Run the allocation
			log.Println("Fish: Allocate the resource using the driver", driver.Name())
			res.HwAddr, res.IpAddr, err = driver.Allocate(string(label.Definition), metadata)
			if err != nil {
				log.Println("Fish: Unable to allocate resource for the Application:", app.UID, err)
				app_state = &types.ApplicationState{ApplicationUID: app.UID, Status: types.ApplicationStateStatusERROR,
					Description: fmt.Sprintf("Driver allocate resource error: %s", err),
				}
			} else {
				err := f.ResourceCreate(res)
				if err != nil {
					log.Println("Fish: Unable to store resource for Application:", app.UID, err)
				}
				app_state = &types.ApplicationState{ApplicationUID: app.UID, Status: types.ApplicationStateStatusALLOCATED,
					Description: fmt.Sprintf("Driver allocated the resource"),
				}
			}
			f.ApplicationStateCreate(app_state)
		}

		// Run the loop to wait for deallocate request
		var deallocate_retry uint8 = 1
		for app_state.Status == types.ApplicationStateStatusALLOCATED {
			if !f.running {
				log.Println("Fish: Stopping the Application execution:", app.UID)
				return
			}
			app_state, err = f.ApplicationStateGetByApplication(app.UID)
			if err != nil {
				log.Println("Fish: Unable to get status for Application:", app.UID, err)
			}
			if app_state.Status == types.ApplicationStateStatusDEALLOCATE || app_state.Status == types.ApplicationStateStatusRECALLED {
				log.Println("Fish: Running Deallocate of the Application:", app.UID)
				// Deallocating and destroy the resource
				if err := driver.Deallocate(res.HwAddr); err != nil {
					log.Printf("Fish: Unable to deallocate the Resource of Application: %s (try: %d): %v\n", app.UID, deallocate_retry, err)
					// Let's retry to deallocate the resource 10 times before give up
					if deallocate_retry <= 10 {
						deallocate_retry += 1
						time.Sleep(10 * time.Second)
						continue
					}
					app_state = &types.ApplicationState{ApplicationUID: app.UID, Status: types.ApplicationStateStatusERROR,
						Description: fmt.Sprintf("Driver deallocate resource error: %s", err),
					}
				} else {
					log.Println("Fish: Successful deallocation of the Application:", app.UID)
					app_state = &types.ApplicationState{ApplicationUID: app.UID, Status: types.ApplicationStateStatusDEALLOCATED,
						Description: fmt.Sprintf("Driver deallocated the resource"),
					}
				}
				// Destroying the resource anyway to not bloat the table - otherwise it will stuck there and
				// will block the access to IP of the other VM's that will reuse this IP
				if err := f.ResourceDelete(res.UID); err != nil {
					log.Println("Fish: Unable to delete Resource for Application:", app.UID, err)
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
				f.node_usage.Subtract(driver.DefinitionResources(string(label.Definition)))
				f.node_usage_mutex.Unlock()
			}

			// Clean the executing application
			f.removeFromExecutingApplincations(app_uid)
		}
		f.applications_mutex.Unlock()

		log.Println("Fish: Done executing Application", app.UID, app_state.Status)
	}()

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
