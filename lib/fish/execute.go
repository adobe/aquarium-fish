/**
 * Copyright 2025 Adobe. All rights reserved.
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
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/adobe/aquarium-fish/lib/drivers"
	"github.com/adobe/aquarium-fish/lib/drivers/provider"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
	"github.com/adobe/aquarium-fish/lib/util"
)

// maybeRunExecuteApplicationStart will run executeApplication if it was not already started
func (f *Fish) maybeRunExecuteApplicationStart(appState *types.ApplicationState) {
	if appState.Status != types.ApplicationStatusELECTED {
		// Applications are going to execution only when they are ELECTED.
		// ALLOCATED ones are running while the node startup so we need strictly ELECTED ones here
		return
	}

	// Check if this node won the election process
	vote := f.wonVotesGetRemove(appState.ApplicationUID)
	if vote == nil {
		return
	}

	log.Info("Fish: Running execution of Application:", appState.ApplicationUID, appState.CreatedAt)

	retry, err := f.executeApplicationStart(vote.ApplicationUID, vote.Available)
	if err == nil {
		// Started successfully, so nothing to worry about
		return
	}

	// The Application execution failed from here

	// Cleanup for executed application
	f.applicationsMutex.Lock()
	delete(f.applications, appState.ApplicationUID)
	f.applicationsMutex.Unlock()

	// If we have retries left for Application - trying to elect the node again
	if retry && f.db.ApplicationStateNewCount(appState.ApplicationUID) <= f.cfg.AllocationRetry {
		log.Warnf("Fish: Can't allocate Application %s, will retry: %v", appState.ApplicationUID, err)

		// Returning Application to the original NEW state
		// to allow the other nodes to try out their luck
		appState = &types.ApplicationState{
			ApplicationUID: appState.ApplicationUID,
			Status:         types.ApplicationStatusNEW,
			Description:    fmt.Sprintf("Failed to run execution on node %s, retry: %v", f.db.GetNodeName(), err),
		}
	} else {
		log.Errorf("Fish: Can't allocate Application %s: %v", appState.ApplicationUID, err)
		appState = &types.ApplicationState{ApplicationUID: appState.ApplicationUID, Status: types.ApplicationStatusERROR,
			Description: fmt.Sprint("Driver allocate resource error:", err),
		}
	}
	if err := f.db.ApplicationStateCreate(appState); err != nil {
		log.Errorf("Fish: Unable to create ApplicationState for Application %s: %v", appState.ApplicationUID, err)
	}
}

func (f *Fish) maybeRunExecuteApplicationStop(appState *types.ApplicationState) {
	if appState.Status != types.ApplicationStatusDEALLOCATE && appState.Status != types.ApplicationStatusRECALLED {
		// Applications are going to execution only when they are ELECTED.
		// ALLOCATED ones are running while the node startup so we need strictly ELECTED ones here
		return
	}

	f.executeApplicationStop(appState.ApplicationUID)
}

// maybeRunApplicationTask is executed on ApplicationTask change and leaves the task to State
// change if the current Application state does not fit the described one in the task.
func (f *Fish) maybeRunApplicationTask(appUID types.ApplicationUID, appTask *types.ApplicationTask) error {
	// Check current Application state
	appState, err := f.db.ApplicationStateGetByApplication(appUID)
	if err != nil {
		return log.Errorf("Fish: Application %s: Task: Unable to get ApplicationState: %v", appUID, err)
	}

	// We can quickly figure out if the Application is in proper state to execute this task or not
	if appTask != nil && appState.Status != appTask.When {
		log.Debugf("Fish: Application %s: Task: Skipping task %q due to wrong state: %q != %q", appUID, appTask.UID, appState.Status, appTask.When)
		return nil
	}

	// Getting ApplicationResource for deallocation
	res, err := f.db.ApplicationResourceGetByApplication(appUID)
	if err != nil {
		return log.Errorf("Fish: Application %s: Task: Unable to find ApplicationResource: %v", appUID, err)
	}

	// Get label with the definitions
	label, err := f.db.LabelGet(res.LabelUID)
	if err != nil {
		return log.Errorf("Fish: Application %s: Task: Unable to find Label %s: %v", appUID, res.LabelUID, err)
	}

	// Extract the Label Definition by the provided index
	if len(label.Definitions) <= res.DefinitionIndex {
		return log.Errorf("Fish: Application %s: Task: The Definition does not exist in the Label %s: %v", appUID, res.LabelUID, res.DefinitionIndex)
	}
	labelDef := label.Definitions[res.DefinitionIndex]

	// Locate the required driver
	driver := drivers.GetProvider(labelDef.Driver)
	if driver == nil {
		return log.Errorf("Fish: Application %s: Task: Unable to locate driver: %s", appUID, labelDef.Driver)
	}

	go func() {
		f.routinesMutex.Lock()
		f.routines.Add(1)
		f.routinesMutex.Unlock()
		defer f.routines.Done()

		// Execute the existing ApplicationTasks on the change
		f.executeApplicationTasks(driver, &labelDef, res, appState.Status)
	}()

	return nil
}

// executeApplication runs the initial and continuous process of the Application allocation.
// First stage should execute relatively quickly (to not get over ping delay), otherwise
// that will cause the cluster to start another round of election. Second stage is executed
// on background and watches the Application till it's deallocated.
func (f *Fish) executeApplicationStart(appUID types.ApplicationUID, defIndex int) (bool, error) {
	log.Debugf("Fish: Application %s: Start: Start executing Application:", appUID.String())

	// Check the application is executed already
	f.applicationsMutex.Lock()
	if _, ok := f.applications[appUID]; ok {
		// Seems the Application is already executing
		f.applicationsMutex.Unlock()
		return false, nil
	}
	// Adding the Application to the executing ones
	f.applications[appUID] = &sync.Mutex{}
	f.applicationsMutex.Unlock()

	// Make sure definition is >= 0 which means it was chosen by the node
	if defIndex < 0 {
		return false, fmt.Errorf("The definition index for Application %s is not chosen: %v", appUID, defIndex)
	}

	app, err := f.db.ApplicationGet(appUID)
	if err != nil {
		return true, fmt.Errorf("Unable to get the Application: %v", err)
	}

	// Check current Application state
	appState, err := f.db.ApplicationStateGetByApplication(app.UID)
	if err != nil {
		return true, fmt.Errorf("Unable to get the Application state: %v", err)
	}

	// Get label with the definitions
	label, err := f.db.LabelGet(app.LabelUID)
	if err != nil {
		return true, fmt.Errorf("Unable to find Label %s: %v", app.LabelUID, err)
	}

	// Extract the Label Definition by the provided index
	if len(label.Definitions) <= defIndex {
		return false, fmt.Errorf("The chosen Definition does not exist in the Label %s: %v (App: %s)", app.LabelUID, defIndex, app.UID)
	}
	labelDef := label.Definitions[defIndex]

	// Locate the required driver
	driver := drivers.GetProvider(labelDef.Driver)
	if driver == nil {
		return true, fmt.Errorf("Unable to locate driver for the Application %s: %s", app.UID, labelDef.Driver)
	}

	// The already running applications will not consume the additional resources
	if appState.Status == types.ApplicationStatusELECTED {
		// In case there are multiple Applications won the election process on the same node it
		// could just have not enough resources, so skip it to allow the other Nodes to try again.
		if !f.isNodeAvailableForDefinition(labelDef) {
			return true, fmt.Errorf("Not enough resources to execute the Application %s", app.UID)
		}
	}

	// If the driver is not using the remote resources - we need to increase the counter
	if !driver.IsRemote() {
		f.nodeUsageMutex.Lock()
		f.nodeUsage.Add(labelDef.Resources)
		f.nodeUsageMutex.Unlock()
	}

	// The main application processing is executed on background because allocation could take a
	// while, after that the bg process will wait for application state change. We do not separate
	// it into method because effectively it could not be running without the logic above.
	go func() {
		f.routinesMutex.Lock()
		f.routines.Add(1)
		f.routinesMutex.Unlock()
		defer f.routines.Done()

		log.Infof("Fish: Application %s: Start: Continuing executing: %s", app.UID, appState.Status)

		// Get or create the new resource object
		var res *types.ApplicationResource
		if appState.Status == types.ApplicationStatusELECTED {
			// Merge application and label metadata, in this exact order
			var mergedMetadata []byte
			var metadata map[string]any
			if err := json.Unmarshal([]byte(app.Metadata), &metadata); err != nil {
				log.Errorf("Fish: Application %s: Start: Unable to parse the Application metadata: %v", app.UID, err)
				appState = &types.ApplicationState{ApplicationUID: app.UID, Status: types.ApplicationStatusERROR,
					Description: fmt.Sprint("Unable to parse the app metadata:", err),
				}
				f.db.ApplicationStateCreate(appState)
			} else if err := json.Unmarshal([]byte(label.Metadata), &metadata); err != nil {
				log.Errorf("Fish: Application %s: Start: Unable to parse the Label metadata: %v", label.UID, err)
				appState = &types.ApplicationState{ApplicationUID: app.UID, Status: types.ApplicationStatusERROR,
					Description: fmt.Sprint("Unable to parse the label metadata:", err),
				}
				f.db.ApplicationStateCreate(appState)
			} else if mergedMetadata, err = json.Marshal(metadata); err != nil {
				log.Errorf("Fish: Application %s: Start: Unable to merge metadata: %v", label.UID, err)
				appState = &types.ApplicationState{ApplicationUID: app.UID, Status: types.ApplicationStatusERROR,
					Description: fmt.Sprint("Unable to merge metadata:", err),
				}
				f.db.ApplicationStateCreate(appState)
			}
			res = &types.ApplicationResource{
				ApplicationUID: app.UID,
				NodeUID:        f.db.GetNodeUID(),
				Metadata:       util.UnparsedJSON(mergedMetadata),
			}
		} else if appState.Status == types.ApplicationStatusALLOCATED {
			res, err = f.db.ApplicationResourceGetByApplication(app.UID)
			if err != nil {
				log.Errorf("Fish: Application %s: Start: Unable to get the allocated Resource: %v", app.UID, err)
				appState = &types.ApplicationState{ApplicationUID: app.UID, Status: types.ApplicationStatusERROR,
					Description: fmt.Sprint("Unable to find the allocated resource:", err),
				}
				f.db.ApplicationStateCreate(appState)
			}
		}

		var metadata map[string]any
		if appState.Status == types.ApplicationStatusELECTED {
			if err := json.Unmarshal([]byte(res.Metadata), &metadata); err != nil {
				log.Errorf("Fish: Application %s: Start: Unable to parse the ApplicationResource metadata: %v", app.UID, err)
				appState = &types.ApplicationState{ApplicationUID: app.UID, Status: types.ApplicationStatusERROR,
					Description: fmt.Sprint("Unable to parse the res metadata:", err),
				}
				f.db.ApplicationStateCreate(appState)
			}
		}

		// Allocate the resource
		if appState.Status == types.ApplicationStatusELECTED {
			// Run the allocation
			log.Infof("Fish: Application %s: Start: Allocate Resource with Label %q (def %d) using driver: %s", app.UID, label.Name, defIndex, driver.Name())
			drvRes, err := driver.Allocate(labelDef, metadata)
			if err != nil {
				// If we have retries left for Application - trying to elect the node again
				retries := f.db.ApplicationStateNewCount(app.UID)
				if retries <= f.cfg.AllocationRetry {
					log.Warnf("Fish: Application %s: Start: Can't allocate, will retry (%d): %v", app.UID, retries, err)

					// Returning Application to the original NEW state
					// to allow the other nodes to try out their luck
					appState = &types.ApplicationState{
						ApplicationUID: app.UID,
						Status:         types.ApplicationStatusNEW,
						Description:    fmt.Sprintf("Failed to allocate Resource on node %s, retry: %v", f.db.GetNodeName(), err),
					}
				} else {
					log.Errorf("Fish: Application %s: Start: Unable to allocate Resource, (tried: %d): %v", app.UID, retries, err)
					appState = &types.ApplicationState{ApplicationUID: app.UID, Status: types.ApplicationStatusERROR,
						Description: fmt.Sprint("Driver allocate resource error:", err),
					}
				}
			} else {
				res.Identifier = drvRes.Identifier
				res.HwAddr = drvRes.HwAddr
				res.IpAddr = drvRes.IpAddr
				res.LabelUID = label.UID
				res.DefinitionIndex = defIndex
				res.Authentication = drvRes.Authentication

				// Getting the resource lifetime to know how much time it will live
				resourceLifetime, err := time.ParseDuration(labelDef.Resources.Lifetime)
				if labelDef.Resources.Lifetime != "" && err != nil {
					log.Errorf("Fish: Application %s: Start: Can't parse the Lifetime from Label: %s (def %d)", app.UID, label.UID, res.DefinitionIndex)
				}
				if err != nil {
					// Try to get default value from fish config
					resourceLifetime = time.Duration(f.cfg.DefaultResourceLifetime)
					if resourceLifetime <= 0 {
						// Not an error - in worst case the resource will just sit there but at least will
						// not ruin the workload execution
						log.Warnf("Fish: Application %s: Start: Default Resource Lifetime is not set in fish config", app.UID)
					}
				}

				if resourceLifetime > 0 {
					timeout := time.Now().Add(resourceLifetime).Round(time.Second)
					res.Timeout = &timeout
				}

				if err = f.db.ApplicationResourceCreate(res); err != nil {
					log.Errorf("Fish: Application %s: Start: Unable to store Resource: %v", app.UID, err)
				}
				appState = &types.ApplicationState{ApplicationUID: app.UID, Status: types.ApplicationStatusALLOCATED,
					Description: "Driver allocated the resource",
				}
				log.Infof("Fish: Application %s: Start: Allocated Resource: %s", app.UID, res.Identifier)
			}
			if err := f.db.ApplicationStateCreate(appState); err != nil {
				log.Errorf("Fish: Application %s: Start: Unable to create ApplicationState: %v", app.UID, err)
			}
		}

		if appState.Status == types.ApplicationStatusALLOCATED {
			if res.Timeout != nil && !res.Timeout.IsZero() {
				f.applicationTimeoutSet(app.UID, *res.Timeout)
			} else {
				log.Warnf("Fish: Application %s: Start: Resource have no lifetime set and will live until deallocated by user", app.UID)
			}
			// Everything went just fine, so returning here
			log.Infof("Fish: Application %s: Start: Completed: %s", app.UID, appState.Status)
			return
		}

		// In case the status was incorrect - cleaning the Application execution
		log.Warnf("Fish: Application %s: Start: Failed to start to execute: %s", app.UID, appState.Status)

		// Decrease the amout of running local apps
		if !driver.IsRemote() {
			f.nodeUsageMutex.Lock()
			f.nodeUsage.Subtract(labelDef.Resources)
			f.nodeUsageMutex.Unlock()
		}

		// Clean the executing application
		f.applicationsMutex.Lock()
		delete(f.applications, app.UID)
		f.applicationsMutex.Unlock()
	}()

	return false, nil
}

func (f *Fish) executeApplicationStop(appUID types.ApplicationUID) error {
	f.applicationsMutex.Lock()
	if _, ok := f.applications[appUID]; !ok {
		// Application is not running by this Node
		f.applicationsMutex.Unlock()
		return nil
	}
	f.applicationsMutex.Unlock()

	// Check current Application state
	log.Debugf("Fish: Application %s: Stop: Stopping the Application", appUID)

	appState, err := f.db.ApplicationStateGetByApplication(appUID)
	if err != nil {
		return log.Errorf("Fish: Application %s: Stop: Unable to get ApplicationState: %v", appUID, err)
	}

	// Getting ApplicationResource for deallocation
	res, err := f.db.ApplicationResourceGetByApplication(appUID)
	if err != nil {
		return log.Errorf("Fish: Application %s: Stop: Unable to find ApplicationResource: %v", appUID, err)
	}

	// Get label with the definitions
	label, err := f.db.LabelGet(res.LabelUID)
	if err != nil {
		return log.Errorf("Fish: Application %s: Stop Unable to find Label %s: %v", appUID, res.LabelUID, err)
	}

	// Extract the Label Definition by the provided index
	if len(label.Definitions) <= res.DefinitionIndex {
		return log.Errorf("Fish: Application %s: Stop The Definition does not exist in the Label %s: %v", appUID, res.LabelUID, res.DefinitionIndex)
	}
	labelDef := label.Definitions[res.DefinitionIndex]

	// Locate the required driver
	driver := drivers.GetProvider(labelDef.Driver)
	if driver == nil {
		return log.Errorf("Fish: Application %s: Stop Unable to locate driver: %s", appUID, labelDef.Driver)
	}

	go func() {
		f.routinesMutex.Lock()
		f.routines.Add(1)
		f.routinesMutex.Unlock()
		defer f.routines.Done()

		// Execute the existing ApplicationTasks. It will be executed prior to executing
		// deallocation by DEALLOCATE & RECALLED which is useful for `snapshot` and `image` tasks.
		f.executeApplicationTasks(driver, &labelDef, res, appState.Status)

		log.Infof("Fish: Application %s: Stop: Running Deallocate of the ApplicationResource:", appUID, res.Identifier)

		// Deallocating and destroy the resource
		for retry := range 20 {
			if err := driver.Deallocate(res); err != nil {
				log.Errorf("Fish: Application %s: Stop: Unable to deallocate the ApplicationResource (try: %d): %v", appUID, retry, err)
				appState = &types.ApplicationState{ApplicationUID: appUID, Status: types.ApplicationStatusERROR,
					Description: fmt.Sprint("Driver deallocate resource error:", err),
				}
				time.Sleep(10 * time.Second)
				continue
			}

			log.Infof("Fish: Application %s: Stop: Successful deallocation of the Application:", appUID)
			appState = &types.ApplicationState{ApplicationUID: appUID, Status: types.ApplicationStatusDEALLOCATED,
				Description: "Driver deallocated the resource",
			}
			break
		}
		// Destroying the resource anyway to not bloat the table - otherwise it will stuck there and
		// will block the access to IP of the other VM's that will reuse this IP
		if err := f.db.ApplicationResourceDelete(res.UID); err != nil {
			log.Errorf("Fish: Application %s: Stop: Unable to delete ApplicationResource: %v", appUID, err)
		}
		if err := f.db.ApplicationStateCreate(appState); err != nil {
			log.Errorf("Fish: Application %s: Stop: Unable to create ApplicationState: %v", appUID, err)
		}

		// Decrease the amout of running local apps
		if !driver.IsRemote() {
			f.nodeUsageMutex.Lock()
			f.nodeUsage.Subtract(labelDef.Resources)
			f.nodeUsageMutex.Unlock()
		}

		// Clean the executing application
		f.applicationsMutex.Lock()
		delete(f.applications, appUID)
		f.applicationsMutex.Unlock()

		log.Infof("Fish: Application %s: Stop: Completed executing of Application: %s", appUID, appState.Status)
	}()

	return nil
}

// executeApplicationTasks will look for all the available ApplicationTasks of the Application and
// execute them if the State of the Application fits
// The important thing here - that the task exec have to be blocking for the Application processes
// that are running - means no other task or deallocation could happen during task execution.
func (f *Fish) executeApplicationTasks(drv provider.Driver, def *types.LabelDefinition, res *types.ApplicationResource, appStatus types.ApplicationStatus) error {
	// Locking specific Application to prevent any other actions to be performed on it
	f.applicationsMutex.Lock()
	lock, ok := f.applications[res.ApplicationUID]
	if !ok {
		// No such Application is executed on the node
		f.applicationsMutex.Unlock()
		return nil
	}
	lock.Lock()
	defer lock.Unlock()
	f.applicationsMutex.Unlock()

	// Execute the associated ApplicationTasks if there is some
	tasks, err := f.db.ApplicationTaskListByApplicationAndWhen(res.ApplicationUID, appStatus)
	if err != nil {
		return log.Errorf("Fish: Application %s: Task: Unable to get ApplicationTasks: %v", res.ApplicationUID, err)
	}
	for _, task := range tasks {
		// Skipping already executed task
		if task.Result != "{}" {
			continue
		}
		t := drv.GetTask(task.Task, string(task.Options))
		if t == nil {
			log.Errorf("Fish: Application %s: Task: Unable to get associated driver task type for Task %q: %v", res.ApplicationUID, task.UID, task.Task)
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
		if err := f.db.ApplicationTaskSave(&task); err != nil {
			log.Errorf("Fish: Application %s: Task: Error during update the task %s with result: %v", res.ApplicationUID, task.UID, err)
		}
	}

	return nil
}

// applicationTimeoutAdd creates another record in Fish list of timeouts to be handled
func (f *Fish) applicationTimeoutSet(uid types.ApplicationUID, to time.Time) {
	f.applicationsTimeoutsMutex.Lock()
	defer f.applicationsTimeoutsMutex.Unlock()

	log.Infof("Fish: Application %s will be deallocated by timeout in %s at %s", uid, time.Until(to).Round(time.Second), to)

	// Checking if the provided timeout is prior to everything else in the timeouts list
	// If one of the timeouts in the list is lower then the new timeout - no need to send update
	needUpdate := true
	for _, appTimeout := range f.applicationsTimeouts {
		if to.After(appTimeout) {
			needUpdate = false
			break
		}
	}

	f.applicationsTimeouts[uid] = to

	if needUpdate {
		// Notifying the process on updated
		f.applicationsTimeoutsUpdated <- struct{}{}
	}
}

// applicationTimeoutProcess watches for the ApplicationResources to make sure they are deallocated
// in time after their lifetime ended. It saves us alot of memory - because we dont need to keep
// alot of goroutines to watch all running Applications with huge context anymore.
func (f *Fish) applicationTimeoutProcess() {
	f.routinesMutex.Lock()
	f.routines.Add(1)
	f.routinesMutex.Unlock()
	defer f.routines.Done()
	defer log.Info("Fish: applicationTimeoutProcess stopped")

	nextApp, nextTimeout := f.applicationTimeoutNext()

	for {
		select {
		case <-f.running.Done():
			return
		case <-f.applicationsTimeoutsUpdated:
			nextApp, nextTimeout = f.applicationTimeoutNext()
		case timeout := <-nextTimeout:
			log.Debugf("Fish: applicationTimeoutProcess: Reached timeout for Application %s", nextApp)
			if nextApp != uuid.Nil {
				log.Warnf("Fish: Application %s reached deadline, sending timeout deallocate", nextApp)
				appState := &types.ApplicationState{
					ApplicationUID: nextApp,
					Status:         types.ApplicationStatusDEALLOCATE,
					Description:    fmt.Sprint("ApplicationResource reached it's timeout:", timeout),
				}
				if err := f.db.ApplicationStateCreate(appState); err != nil {
					log.Errorf("Fish: Unable to create ApplicationState for Application %s: %v", nextApp, err)
				}
				f.applicationsTimeoutsMutex.Lock()
				delete(f.applicationsTimeouts, nextApp)
				f.applicationsTimeoutsMutex.Unlock()
			}
			// Calling for the next patient
			nextApp, nextTimeout = f.applicationTimeoutNext()
		}
	}
}

// applicationTimeoutNext returns next closest timeout from the list or 1h
func (f *Fish) applicationTimeoutNext() (uid types.ApplicationUID, to <-chan time.Time) {
	f.applicationsTimeoutsMutex.Lock()
	defer f.applicationsTimeoutsMutex.Unlock()

	var minTime = time.Now().Add(time.Hour)

	for appUID, timeout := range f.applicationsTimeouts {
		if minTime.After(timeout) {
			uid = appUID
			minTime = timeout
		}
	}

	log.Debugf("Fish: applicationTimeoutProcess: Next timeout for Application %s at %s", uid, minTime)

	return uid, time.After(time.Until(minTime))
}
