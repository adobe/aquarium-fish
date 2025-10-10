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

// Author: Sergei Parshev (@sparshev)

package fish

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/adobe/aquarium-fish/lib/drivers"
	"github.com/adobe/aquarium-fish/lib/drivers/provider"
	"github.com/adobe/aquarium-fish/lib/log"
	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/util"
)

// maybeRunExecuteApplicationStart will run executeApplication if it was not already started
func (f *Fish) maybeRunExecuteApplicationStart(appState *typesv2.ApplicationState) {
	if appState.Status != typesv2.ApplicationState_ELECTED {
		// Applications are going to execution only when they are ELECTED.
		// ALLOCATED ones are running while the node startup so we need strictly ELECTED ones here
		return
	}

	ctx := context.Background()
	logger := log.WithFunc("fish", "maybeRunExecuteApplicationStart").With("app_uid", appState.ApplicationUid)

	// Check if this node won the election process
	vote := f.wonVotesGetRemove(appState.ApplicationUid)
	if vote == nil {
		return
	}

	logger.InfoContext(ctx, "Running execution of Application", "created_at", appState.CreatedAt)

	retry, err := f.executeApplicationStart(vote.ApplicationUid, vote.Available)
	if err == nil {
		// Started successfully, so nothing to worry about
		return
	}

	// The Application execution failed from here

	// Cleanup for executed application
	f.applicationsMutex.Lock()
	lock := f.applications[appState.ApplicationUid]
	delete(f.applications, appState.ApplicationUid)
	f.applicationsMutex.Unlock()
	lock.Unlock()

	// If we have retries left for Application - trying to elect the node again
	if retry && f.db.ApplicationStateNewCount(ctx, appState.ApplicationUid) <= f.cfg.AllocationRetry {
		logger.WarnContext(ctx, "Can't allocate Application, will retry...", "err", err)

		// Returning Application to the original NEW state
		// to allow the other nodes to try out their luck
		appState = &typesv2.ApplicationState{
			ApplicationUid: appState.ApplicationUid,
			Status:         typesv2.ApplicationState_NEW,
			Description:    fmt.Sprintf("Failed to run execution on node %s, retry: %v", f.db.GetNodeName(), err),
		}
	} else {
		logger.ErrorContext(ctx, "Can't allocate Application", "err", err)
		appState = &typesv2.ApplicationState{
			ApplicationUid: appState.ApplicationUid, Status: typesv2.ApplicationState_ERROR,
			Description: fmt.Sprint("Driver allocate resource error:", err),
		}
	}
	if err := f.db.ApplicationStateCreate(ctx, appState); err != nil {
		logger.ErrorContext(ctx, "Unable to create ApplicationState for Application", "err", err)
	}
}

func (f *Fish) maybeRunExecuteApplicationStop(appState *typesv2.ApplicationState) {
	if appState.Status != typesv2.ApplicationState_DEALLOCATE {
		// Application stop is possible only when it's got DEALLOCATE request.
		return
	}

	f.executeApplicationStop(appState.ApplicationUid)
}

// maybeRunApplicationTask is executed on ApplicationTask change and leaves the task to State
// change if the current Application state does not fit the described one in the task.
func (f *Fish) maybeRunApplicationTask(appUID typesv2.ApplicationUID, appTask *typesv2.ApplicationTask) error {
	ctx := context.Background()
	logger := log.WithFunc("fish", "maybeRunApplicationTask").With("app_uid", appUID)
	// Check current Application state
	appState, err := f.db.ApplicationStateGetByApplication(ctx, appUID)
	if err != nil {
		logger.Error("Task: Unable to get ApplicationState", "err", err)
		return fmt.Errorf("Fish: Application %s: Task: Unable to get ApplicationState: %v", appUID, err)
	}

	// We can quickly figure out if the Application is in proper state to execute this task or not
	if appTask != nil && appState.Status != appTask.When {
		logger.Debug("Task: Skipping task due to wrong state", "app_status", appState.Status, "apptask_when", appTask.When)
		return nil
	}

	// Getting ApplicationResource to execute a task on it - if it's not here, it's not a big deal,
	// because the Application could be not allocated yet, so have no resource and we need to skip.
	res, err := f.db.ApplicationResourceGetByApplication(ctx, appUID)
	if err != nil {
		logger.Info("Task: Skipping since no ApplicationResource found", "err", err)
		return nil
	}

	// Get label with the definitions
	label, err := f.db.LabelGet(ctx, res.LabelUid)
	if err != nil {
		logger.Error("Task: Unable to find Label", "err", err)
		return fmt.Errorf("Fish: Application %s: Task: Unable to find Label %s: %v", appUID, res.LabelUid, err)
	}

	// Extract the Label Definition by the provided index
	if len(label.Definitions) <= int(res.DefinitionIndex) {
		logger.Error("Task: The Definition does not exist in the Label", "label_uid", res.LabelUid, "definition_index", res.DefinitionIndex)
		return fmt.Errorf("Fish: Application %s: Task: The Definition does not exist in the Label %s: %v", appUID, res.LabelUid, res.DefinitionIndex)
	}
	labelDef := label.Definitions[res.DefinitionIndex]

	// Locate the required driver
	driver := drivers.GetProvider(labelDef.Driver)
	if driver == nil {
		logger.Error("Task: Unable to locate driver", "driver", labelDef.Driver)
		return fmt.Errorf("Fish: Application %s: Task: Unable to locate driver: %s", appUID, labelDef.Driver)
	}

	go func() {
		f.routinesMutex.Lock()
		f.routines.Add(1)
		f.routinesMutex.Unlock()
		defer f.routines.Done()
		defer logger.Info("executeApplicationTasks stopped")

		// Execute the existing ApplicationTasks on the change
		f.executeApplicationTasks(ctx, driver, &labelDef, res, appState.Status)
	}()

	return nil
}

// executeApplication runs the initial and continuous process of the Application allocation.
// First stage should execute relatively quickly (to not get over ping delay), otherwise
// that will cause the cluster to start another round of election. Second stage is executed
// on background and watches the Application till it's deallocated.
func (f *Fish) executeApplicationStart(appUID typesv2.ApplicationUID, defIndex int32) (bool, error) {
	ctx := context.Background()
	logger := log.WithFunc("fish", "executeApplicationStart").With("app_uid", appUID)
	logger.Debug("Start executing Application")

	// Check the application is executed already
	f.applicationsMutex.Lock()
	if _, ok := f.applications[appUID]; ok {
		// Seems the Application is already executing
		f.applicationsMutex.Unlock()
		return false, nil
	}
	// Adding the Application to the executing ones
	f.applications[appUID] = &sync.Mutex{}
	lock := f.applications[appUID]
	f.applicationsMutex.Unlock()

	// Locking the application since it's in process
	// It will be unlocked in maybeRunExecuteApplicationStart function
	lock.Lock()

	// Make sure definition is >= 0 which means it was chosen by the node
	if defIndex < 0 {
		return false, fmt.Errorf("The definition index for Application %s is not chosen: %v", appUID, defIndex)
	}

	app, err := f.db.ApplicationGet(ctx, appUID)
	if err != nil {
		return true, fmt.Errorf("Unable to get the Application: %v", err)
	}

	// Check current Application state
	appState, err := f.db.ApplicationStateGetByApplication(ctx, app.Uid)
	if err != nil {
		return true, fmt.Errorf("Unable to get the Application state: %v", err)
	}

	// Need to check if the Application is active, otherwise just stop execution
	if !f.db.ApplicationStateIsActive(appState.Status) {
		return false, fmt.Errorf("Not active Application state: %s", appState.Status)
	}

	// Get label with the definitions
	label, err := f.db.LabelGet(ctx, app.LabelUid)
	if err != nil {
		return true, fmt.Errorf("Unable to find Label %s: %v", app.LabelUid, err)
	}

	// Extract the Label Definition by the provided index
	if len(label.Definitions) <= int(defIndex) {
		return false, fmt.Errorf("The chosen Definition does not exist in the Label %s: %v (App: %s)", app.LabelUid, defIndex, app.Uid)
	}
	labelDef := label.Definitions[defIndex]

	// Locate the required driver
	driver := drivers.GetProvider(labelDef.Driver)
	if driver == nil {
		return true, fmt.Errorf("Unable to locate driver for the Application %s: %s", app.Uid, labelDef.Driver)
	}

	// The already running applications will not consume the additional resources
	if appState.Status == typesv2.ApplicationState_ELECTED {
		// In case there are multiple Applications won the election process on the same node it
		// could just have not enough resources, so skip it to allow the other Nodes to try again.
		if !f.isNodeAvailableForDefinition(labelDef) {
			return true, fmt.Errorf("Not enough resources to execute the Application %s", app.Uid)
		}
	}

	// If the driver is not using the remote resources - we need to increase the counter
	if !driver.IsRemote() {
		f.nodeUsageMutex.Lock()
		f.nodeUsage.Add(&labelDef.Resources)
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
		defer logger.Info("executeApplicationStart stopped")
		defer lock.Unlock()

		logger.Info("Continuing execute", "appstate_status", appState.Status)

		// Get or create the new resource object
		var res *typesv2.ApplicationResource
		if appState.Status == typesv2.ApplicationState_ELECTED {
			// Merge application and label metadata, in this exact order
			var mergedMetadata []byte
			var metadata map[string]any
			if err := json.Unmarshal([]byte(app.Metadata), &metadata); err != nil {
				logger.Error("Unable to parse the Application metadata", "err", err)
				appState = &typesv2.ApplicationState{
					ApplicationUid: app.Uid, Status: typesv2.ApplicationState_ERROR,
					Description: fmt.Sprint("Unable to parse the app metadata:", err),
				}
				f.db.ApplicationStateCreate(ctx, appState)
			} else if err := json.Unmarshal([]byte(label.Metadata), &metadata); err != nil {
				logger.Error("Unable to parse the Label metadata", "err", err, "label_uid", label.Uid)
				appState = &typesv2.ApplicationState{
					ApplicationUid: app.Uid, Status: typesv2.ApplicationState_ERROR,
					Description: fmt.Sprint("Unable to parse the label metadata:", err),
				}
				f.db.ApplicationStateCreate(ctx, appState)
			} else if mergedMetadata, err = json.Marshal(metadata); err != nil {
				logger.Error("Unable to merge metadata", "err", err, "label_uid", label.Uid)
				appState = &typesv2.ApplicationState{
					ApplicationUid: app.Uid, Status: typesv2.ApplicationState_ERROR,
					Description: fmt.Sprint("Unable to merge metadata:", err),
				}
				f.db.ApplicationStateCreate(ctx, appState)
			}
			res = &typesv2.ApplicationResource{
				ApplicationUid: app.Uid,
				NodeUid:        f.db.GetNodeUID(),
				Metadata:       util.UnparsedJSON(mergedMetadata),
			}
		} else if appState.Status == typesv2.ApplicationState_ALLOCATED {
			res, err = f.db.ApplicationResourceGetByApplication(ctx, app.Uid)
			if err != nil {
				logger.Error("Unable to get the allocated Resource", "err", err)
				appState = &typesv2.ApplicationState{
					ApplicationUid: app.Uid, Status: typesv2.ApplicationState_ERROR,
					Description: fmt.Sprint("Unable to find the allocated resource:", err),
				}
				f.db.ApplicationStateCreate(ctx, appState)
			}
		}

		var metadata map[string]any
		if appState.Status == typesv2.ApplicationState_ELECTED {
			if err := json.Unmarshal([]byte(res.Metadata), &metadata); err != nil {
				logger.Error("Unable to parse the ApplicationResource metadata", "err", err)
				appState = &typesv2.ApplicationState{
					ApplicationUid: app.Uid, Status: typesv2.ApplicationState_ERROR,
					Description: fmt.Sprint("Unable to parse the res metadata:", err),
				}
				f.db.ApplicationStateCreate(ctx, appState)
			}
		}

		// Allocate the resource
		if appState.Status == typesv2.ApplicationState_ELECTED {
			// Run the allocation
			logger.Info("Allocate Resource", "label_name", label.Name, "definition_index", defIndex, "driver_name", driver.Name())
			drvRes, err := driver.Allocate(labelDef, metadata)
			if err != nil {
				// If we have retries left for Application - trying to elect the node again
				retries := f.db.ApplicationStateNewCount(ctx, app.Uid)
				if retries <= f.cfg.AllocationRetry {
					logger.Warn("Can't allocate, will retry...", "retries", retries, "err", err)

					// Returning Application to the original NEW state
					// to allow the other nodes to try out their luck
					appState = &typesv2.ApplicationState{
						ApplicationUid: app.Uid,
						Status:         typesv2.ApplicationState_NEW,
						Description:    fmt.Sprintf("Failed to allocate Resource on node %s, retry: %v", f.db.GetNodeName(), err),
					}
				} else {
					logger.Error("Unable to allocate Resource", "retries", retries, "err", err)
					appState = &typesv2.ApplicationState{
						ApplicationUid: app.Uid, Status: typesv2.ApplicationState_ERROR,
						Description: fmt.Sprint("Driver allocate resource error:", err),
					}
				}
			} else {
				res.Identifier = drvRes.Identifier
				res.HwAddr = drvRes.HwAddr
				res.IpAddr = drvRes.IpAddr
				res.LabelUid = label.Uid
				res.DefinitionIndex = defIndex
				res.Authentication = drvRes.Authentication

				// Getting the resource lifetime to know how much time it will live
				resourceLifetime := time.Duration(f.cfg.DefaultResourceLifetime) // Using fish node default
				if resourceLifetime <= 0 {
					// Not an error - in worst case the resource will just sit there but at least will
					// not ruin the workload execution
					logger.Warn("Default Resource Lifetime is not set in fish config")
				}
				if labelDef.Resources.Lifetime != nil && *labelDef.Resources.Lifetime != "" {
					labelLifetime, err := time.ParseDuration(*labelDef.Resources.Lifetime)
					if err != nil {
						logger.Error("Can't parse the Lifetime from Label", "label_uid", label.Uid, "res_def_index", res.DefinitionIndex, "err", err)
					} else {
						resourceLifetime = labelLifetime
					}
				}

				if resourceLifetime > 0 {
					timeout := time.Now().Add(resourceLifetime).Round(time.Second)
					res.Timeout = &timeout
				}

				if err = f.db.ApplicationResourceCreate(ctx, res); err != nil {
					logger.Error("Unable to store Resource", "err", err)
				}
				appState = &typesv2.ApplicationState{
					ApplicationUid: app.Uid, Status: typesv2.ApplicationState_ALLOCATED,
					Description: "Driver allocated the resource",
				}
				logger.Info("Allocated Resource", "res_identifier", res.Identifier)
			}
			if err := f.db.ApplicationStateCreate(ctx, appState); err != nil {
				logger.Error("Unable to create ApplicationState", "err", err)
			}
		}

		if appState.Status == typesv2.ApplicationState_ALLOCATED {
			if res.Timeout != nil && !res.Timeout.IsZero() {
				f.applicationTimeoutSet(app.Uid, *res.Timeout)
			} else {
				logger.Warn("Resource have no lifetime set and will live until deallocated by user")
			}
			// Everything went just fine, so returning here
			logger.Info("Completed", "appstate_status", appState.Status)
			return
		}

		// In case the status was incorrect - cleaning the Application execution
		logger.Warn("Failed to start to execute", "appstate_status", appState.Status)

		// Decrease the amout of running local apps
		if !driver.IsRemote() {
			f.nodeUsageMutex.Lock()
			f.nodeUsage.Subtract(&labelDef.Resources)
			f.nodeUsageMutex.Unlock()
		}

		// Clean the executing application
		f.applicationsMutex.Lock()
		delete(f.applications, app.Uid)
		f.applicationsMutex.Unlock()
	}()

	return false, nil
}

func (f *Fish) executeApplicationStop(appUID typesv2.ApplicationUID) error {
	ctx := context.Background()
	logger := log.WithFunc("fish", "executeApplicationStop").With("app_uid", appUID)
	f.applicationsMutex.Lock()
	lock, ok := f.applications[appUID]
	if !ok {
		// Application is not running by this Node
		f.applicationsMutex.Unlock()
		return nil
	}
	f.applicationsMutex.Unlock()

	// Locking Application in transition state
	lock.Lock()
	defer lock.Unlock()

	// Check current Application state
	logger.Debug("Stopping the Application")

	appState, err := f.db.ApplicationStateGetByApplication(ctx, appUID)
	if err != nil {
		logger.Error("Unable to get ApplicationState", "err", err)
		return fmt.Errorf("Fish: Application %s: Stop: Unable to get ApplicationState: %v", appUID, err)
	}

	// Getting ApplicationResource for deallocation
	res, err := f.db.ApplicationResourceGetByApplication(ctx, appUID)
	if err != nil {
		logger.Error("Unable to find ApplicationResource", "err", err)
		return fmt.Errorf("Fish: Application %s: Stop: Unable to find ApplicationResource: %v", appUID, err)
	}

	// Get label with the definitions
	label, err := f.db.LabelGet(ctx, res.LabelUid)
	if err != nil {
		logger.Error("Unable to find Label", "label_uid", res.LabelUid, "err", err)
		return fmt.Errorf("Fish: Application %s: Stop Unable to find Label %s: %v", appUID, res.LabelUid, err)
	}

	// Extract the Label Definition by the provided index
	if len(label.Definitions) <= int(res.DefinitionIndex) {
		logger.Error("The Definition does not exist in the Label", "label_uid", res.LabelUid, "res_def_index", res.DefinitionIndex)
		return fmt.Errorf("Fish: Application %s: Stop The Definition does not exist in the Label %s: %v", appUID, res.LabelUid, res.DefinitionIndex)
	}
	labelDef := label.Definitions[res.DefinitionIndex]

	// Locate the required driver
	driver := drivers.GetProvider(labelDef.Driver)
	if driver == nil {
		logger.Error("Unable to locate driver", "driver", labelDef.Driver)
		return fmt.Errorf("Fish: Application %s: Stop Unable to locate driver: %s", appUID, labelDef.Driver)
	}

	go func() {
		f.routinesMutex.Lock()
		f.routines.Add(1)
		f.routinesMutex.Unlock()
		defer f.routines.Done()
		defer logger.Info("executeApplicationStop completed")

		// Execute the existing ApplicationTasks. It will be executed prior to executing
		// deallocation by DEALLOCATE which is useful for `snapshot` and `image` tasks.
		f.executeApplicationTasks(ctx, driver, &labelDef, res, appState.Status)

		// Locking the application transition state
		lock.Lock()
		defer lock.Unlock()
		logger.Info("Running Deallocate of the ApplicationResource", "res_identifier", res.Identifier)

		// Deallocating and destroy the resource
		for retry := range 20 {
			if err := driver.Deallocate(*res); err != nil {
				logger.Error("Unable to deallocate the ApplicationResource", "retry", retry, "err", err)
				appState = &typesv2.ApplicationState{
					ApplicationUid: appUID, Status: typesv2.ApplicationState_ERROR,
					Description: fmt.Sprint("Driver deallocate resource error:", err),
				}
				time.Sleep(10 * time.Second)
				continue
			}

			logger.Info("Application deallocated successfully")
			appState = &typesv2.ApplicationState{
				ApplicationUid: appUID, Status: typesv2.ApplicationState_DEALLOCATED,
				Description: "Driver deallocated the resource",
			}
			// We don't need timeout anymore
			f.applicationTimeoutRemove(appUID)
			break
		}
		// Destroying the resource anyway to not bloat the table - otherwise it will stuck there and
		// will block the access to IP of the other VM's that will reuse this IP
		if err := f.db.ApplicationResourceDelete(ctx, res.Uid); err != nil {
			logger.Error("Unable to delete ApplicationResource", "err", err)
		}
		if err := f.db.ApplicationStateCreate(ctx, appState); err != nil {
			logger.Error("Unable to create ApplicationState", "err", err)
		}

		// Decrease the amout of running local apps
		if !driver.IsRemote() {
			f.nodeUsageMutex.Lock()
			f.nodeUsage.Subtract(&labelDef.Resources)
			f.nodeUsageMutex.Unlock()
		}

		// Clean the executing application
		f.applicationsMutex.Lock()
		delete(f.applications, appUID)
		f.applicationsMutex.Unlock()

		logger.Info("Completed executing of Application", "app_status", appState.Status)
	}()

	return nil
}

// executeApplicationTasks will look for all the available ApplicationTasks of the Application and
// execute them if the State of the Application fits
// The important thing here - that the task exec have to be blocking for the Application processes
// that are running - means no other task or deallocation could happen during task execution.
func (f *Fish) executeApplicationTasks(ctx context.Context, drv provider.Driver, def *typesv2.LabelDefinition, res *typesv2.ApplicationResource, appStatus typesv2.ApplicationState_Status) error {
	// Locking specific Application to prevent any other actions to be performed on it
	f.applicationsMutex.Lock()
	lock, ok := f.applications[res.ApplicationUid]
	if !ok {
		// No such Application is executed on the node
		f.applicationsMutex.Unlock()
		return nil
	}
	f.applicationsMutex.Unlock()

	logger := log.WithFunc("fish", "executeApplicationTasks").With("app_uid", res.ApplicationUid, "app_status", appStatus)

	// Locking Application in task execution
	lock.Lock()
	defer lock.Unlock()

	// Execute the associated ApplicationTasks if there is some
	tasks, err := f.db.ApplicationTaskListByApplicationAndWhen(ctx, res.ApplicationUid, appStatus)
	if err != nil {
		logger.Error("Unable to get ApplicationTasks", "err", err)
		return fmt.Errorf("Fish: Application %s: Task: Unable to get ApplicationTasks: %v", res.ApplicationUid, err)
	}
	for _, task := range tasks {
		tasklogger := logger.With("task", task.Task, "task_uid", task.Uid)
		// Skipping already executed task
		if task.Result != "{}" {
			continue
		}
		t := drv.GetTask(task.Task, string(task.Options))
		if t == nil {
			tasklogger.Error("Unable to get associated driver task type")
			task.Result = util.UnparsedJSON(`{"error":"task not available in driver"}`)
		} else {
			tasklogger.Debug("Executing task")
			// Executing the task
			t.SetInfo(&task, def, res)
			result, err := t.Execute()
			if err != nil {
				// We're not crashing here because even with error task could have a result
				tasklogger.Error("Error happened during executing the task", "err", err)
			}
			task.Result = util.UnparsedJSON(result)
			tasklogger.Debug("Executing task completed")
		}
		if err := f.db.ApplicationTaskSave(ctx, &task); err != nil {
			tasklogger.Error("Error during update the task with result", "err", err)
		}
	}

	return nil
}

// applicationTimeoutSet creates another record in Fish list of timeouts to be handled
func (f *Fish) applicationTimeoutSet(uid typesv2.ApplicationUID, to time.Time) {
	f.applicationsTimeoutsMutex.Lock()
	defer f.applicationsTimeoutsMutex.Unlock()

	logger := log.WithFunc("fish", "applicationTimeoutSet").With("app_uid", uid)
	logger.Info("Application will be deallocated by timeout", "in", time.Until(to).Round(time.Second), "timeout", to)

	// Checking if the provided timeout is prior to everything else in the timeouts list
	// If one of the timeouts in the list is earlier then the new timeout - no need to send update
	needUpdate := true
	for _, appTimeout := range f.applicationsTimeouts {
		if to.After(appTimeout) {
			needUpdate = false
			break
		}
	}

	f.applicationsTimeouts[uid] = to

	if needUpdate {
		// Notifying the process on updated in background to not block the process execution
		go func() {
			f.applicationsTimeoutsUpdated <- struct{}{}
		}()
	}
}

// applicationTimeoutRemove clears the timeout event for provided Application from the map
func (f *Fish) applicationTimeoutRemove(uid typesv2.ApplicationUID) {
	f.applicationsTimeoutsMutex.Lock()
	defer f.applicationsTimeoutsMutex.Unlock()

	to, ok := f.applicationsTimeouts[uid]
	if !ok {
		// Apparently timeout is not here, so nothing to worry about
		return
	}

	delete(f.applicationsTimeouts, uid)

	// Checking if the known timeout is prior to everything else in the timeouts list
	// If one of the timeouts in the list is earlier then the new timeout - no need to send update
	needUpdate := true
	for _, appTimeout := range f.applicationsTimeouts {
		if to.After(appTimeout) {
			needUpdate = false
			break
		}
	}

	if needUpdate {
		// Notifying the process on updated in background to not block the process execution
		go func() {
			f.applicationsTimeoutsUpdated <- struct{}{}
		}()
	}
}

// applicationTimeoutProcess watches for the ApplicationResources to make sure they are deallocated
// in time after their lifetime ended. It saves us alot of memory - because we dont need to keep
// alot of goroutines to watch all running Applications with huge context anymore.
func (f *Fish) applicationTimeoutProcess(ctx context.Context) {
	f.routinesMutex.Lock()
	f.routines.Add(1)
	f.routinesMutex.Unlock()
	defer f.routines.Done()
	logger := log.WithFunc("fish", "applicationTimeoutProcess")
	defer logger.Info("applicationTimeoutProcess stopped")

	appUID, appTimeout := f.applicationTimeoutNext()

	for {
		select {
		case <-f.running.Done():
			return
		case <-f.applicationsTimeoutsUpdated:
			appUID, appTimeout = f.applicationTimeoutNext()
		case timeout := <-appTimeout:
			applogger := logger.With("app_uid", appUID)
			applogger.Debug("Reached timeout for Application")
			if appUID != uuid.Nil {
				// We need to check Application is still allocated before deallocation
				appState, err := f.db.ApplicationStateGetByApplication(ctx, appUID)
				if err != nil {
					applogger.Debug("Can't find Application to timeout", "err", err)
				} else if !f.db.ApplicationStateIsActive(appState.Status) {
					applogger.Debug("Application is not active to timeout")
				} else {
					applogger.Warn("Application reached deadline, sending timeout deallocate")
					appState = &typesv2.ApplicationState{
						ApplicationUid: appUID,
						Status:         typesv2.ApplicationState_DEALLOCATE,
						Description:    fmt.Sprint("ApplicationResource reached it's timeout:", timeout),
					}
					if err := f.db.ApplicationStateCreate(ctx, appState); err != nil {
						applogger.Error("Application unable to create ApplicationState", "err", err)
					}
				}
				f.applicationsTimeoutsMutex.Lock()
				delete(f.applicationsTimeouts, appUID)
				f.applicationsTimeoutsMutex.Unlock()
			}
			// Calling for the next patient
			appUID, appTimeout = f.applicationTimeoutNext()
		}
	}
}

// applicationTimeoutNext returns next closest timeout from the list or 1h
func (f *Fish) applicationTimeoutNext() (uid typesv2.ApplicationUID, to <-chan time.Time) {
	f.applicationsTimeoutsMutex.Lock()
	defer f.applicationsTimeoutsMutex.Unlock()

	minTime := time.Now().Add(time.Hour)

	for appUID, timeout := range f.applicationsTimeouts {
		if minTime.After(timeout) {
			uid = appUID
			minTime = timeout
		}
	}

	log.WithFunc("fish", "applicationTimeoutNext").Debug("Next timeout for Application at", "app_uid", uid, "timeout", minTime)

	return uid, time.After(time.Until(minTime))
}
