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

package github

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/go-github/v71/github"

	"github.com/adobe/aquarium-fish/lib/log"
)

// validateDelivery will ensure that delivery is valid and was not processed before
func (d *Driver) validateDelivery(delivery *github.HookDelivery) bool {
	// We accepting only workflow_job events
	if delivery.GetEvent() != "workflow_job" {
		return false
	}

	// We don't need to process webhooks that was successfully delivered through webhook push
	if d.cfg.isWebhookEnabled() && delivery.GetStatusCode() == 200 /*OK*/ {
		return false
	}

	// Quickly check if such webhook was already processed
	d.webhooksMutex.Lock()
	defer d.webhooksMutex.Unlock()
	return !d.isWebhookProcessed(delivery.GetGUID())
}

// checkDeliveries verifies happened deliveries
// It will be skipped if Pull by API is not configured
// It will run on schedule if gate is configured only for Pull by API
// It will run on schedule if gate is configured for both Push and Pull
func (d *Driver) checkDeliveries() (outerr error) {
	logger := log.WithFunc("github", "checkDeliveries").With("gate.name", d.name)
	d.hooksMutex.RLock()
	defer d.hooksMutex.RUnlock()

	logger.Debug("Checking deliveries...")
	defer logger.Debug("Checking deliveries done")

	var checkpointUpdate time.Time
	checkpointUpdated := false

	for _, hook := range d.hooks {
		logger.Debug("Processing hook", "hook_url", hook.GetURL())
		// Getting repo name from the webhook URL
		spl := strings.Split(hook.GetURL(), "/")
		if len(spl) < 8 {
			logger.Error("Not enough parameters in webhook URL", "hook_url", hook.GetURL())
			outerr = fmt.Errorf("GITHUB: %s: Not enough parameters in webhook URL: %s", d.name, hook.GetURL())
			continue
		}
		owner := spl[len(spl)-4]
		repo := spl[len(spl)-3]
		repoName := fmt.Sprintf("%s/%s", owner, repo)

		deliveries, err := d.apiGetDeliveriesList(owner, repo, hook.GetID())
		if err != nil {
			logger.Error("Repo hook deliveries list request", "repo", repoName, "hook_id", hook.GetID(), "err", err)
			outerr = fmt.Errorf("GITHUB: %s: Repo %q hook %d deliveries list request: %v", d.name, repoName, hook.GetID(), err)
		}
		if len(deliveries) == 0 {
			logger.Debug("Repo hook no new deliveries found", "repo", repoName, "hook_id", hook.GetID())
			continue
		}

		// Checkpoint will be updated to last delivery (will be first in line) of the first hook
		// That is needed to stick to github.com time rather then rely on current host time sync
		if !checkpointUpdated {
			t := deliveries[0].DeliveredAt.GetTime()
			checkpointUpdate = *t
			checkpointUpdated = true
		}

		// Starting background process to deal with the received deliveries
		go d.processHookDeliveries(deliveries, owner, repo, hook.GetID())
	}

	// Set the checkpoint to the last delivery of the first hook
	if !checkpointUpdate.IsZero() {
		// We need to add 1 microsecond here because we comparing it to delivery as After
		d.apiCheckpoint = checkpointUpdate.Add(1)
		logger.Debug("Updated deliveries checkpoint", "checkpoint", d.apiCheckpoint)
	}

	return outerr
}

// processHookDeliveries is needed to execute a bunch of webhooks in the right order and remove
// the webhooks that are cancelling each other (when there is "queued" and "completed" webhook)
// This function running in a separated goroutine, so just prints out the result
func (d *Driver) processHookDeliveries(deliveries []*github.HookDelivery, owner, repo string, hookID int64) error {
	logger := log.WithFunc("github", "processHookDeliveries").With("gate.name", d.name, "repo", owner+"/"+repo, "hook_id", hookID)
	var jobs []*github.WorkflowJob
	for _, delivery := range deliveries {
		logger.Debug("Getting full delivery for webhook", "delivery", delivery)

		// Receiving full delivery body
		fullDelivery, err := d.apiGetFullDelivery(owner, repo, hookID, delivery.GetID())
		if err != nil {
			logger.Error("Repo full delivery request", "delivery_guid", delivery.GetGUID(), "err", err)
			return fmt.Errorf("GITHUB: %s: Repo %s/%s full delivery %s request: %v", d.name, owner, repo, delivery.GetGUID(), err)
		}

		// Extracting job fom webhook request with optional verification of the secret if
		// it's not set - because we already reading from github.com, so should be enough for
		// proper security measures - if we don't trust github.com, then we have a problem
		if job, err := d.extractJob(fullDelivery.Request, false); err != nil {
			logger.Error("Error processing repo webhook request", "delivery_guid", delivery.GetGUID(), "err", err)
		} else if job != nil {
			jobs = append(jobs, job)
		}
	}

	// Optimization to filter jobs and cancel-out the completed and queued ones with the same IDs
	var filteredJobs []*github.WorkflowJob
	var completed []*github.WorkflowJob
	for _, job := range jobs {
		if job.GetStatus() == jobCompleted {
			completed = append(completed, job)
		}
	}
	// We collected completed jobs, now needed to cancel out the jobs in the same id/runid
	// At the same time we going in reverse to execute jobs from oldest to newest
	// The algorithm passes dup completed jobs, but since they were not queued - no issues here
	for i := len(jobs) - 1; i >= 0; i-- {
		job := jobs[i]
		nomatch := true
		// Processing only queued jobs
		if job.GetStatus() == jobQueued {
			for _, compJob := range completed {
				if compJob.GetID() == job.GetID() && compJob.GetRunID() == job.GetRunID() {
					logger.Debug("Cancelling out queued-completed job", "job", fmt.Sprintf("%d-%d", job.GetRunID(), job.GetID()))
					nomatch = false
				}
			}
		}
		if nomatch {
			filteredJobs = append(filteredJobs, job)
		}
	}

	// Now we have reversed and filtered list of jobs to execute

	for _, job := range filteredJobs {
		// Processing webhook request with optional verification of the secret if
		// it's not set - because we already reading from github.com, so should be enough for
		// proper security measures - if we don't trust github.com, then we have a problem
		if err := d.executeJob(owner, repo, job); err != nil {
			logger.Error("Error executing job for repo run-job", "job", fmt.Sprintf("%d-%d", job.GetRunID(), job.GetID()), "err", err)
		}
	}

	return nil
}

// updateHooks is needed to get all the available webhooks from the repos we have
// It will be skipped if Pull by API is not configured
// It will run on schedule if gate is configured only for Pull by API
// It will run only during initialization if gate configured for both Push and Pull
func (d *Driver) updateHooks() error {
	logger := log.WithFunc("github", "updateHooks").With("gate.name", d.name)
	logger.Debug("Updating hooks...")
	defer logger.Debug("Updating hooks done")

	repos, err := d.apiGetRepos()
	if err != nil {
		logger.Warn("Unable to get all available repositories", "err", err)
	}
	if len(repos) == 0 {
		logger.Error("No available repositories found", "repos", len(repos))
		return nil
	}

	var updatedHooks []*github.Hook

	for _, repoName := range repos {
		spl := strings.SplitN(repoName, "/", 2)
		if len(spl) < 2 {
			logger.Error("Incorrect repo full name", "repo", repoName)
			continue
		}

		hooks, err := d.apiGetHooks(spl[0], spl[1])
		if err != nil {
			logger.Error("Repo hooks request", "repo", repoName, "err", err)
			return fmt.Errorf("GITHUB: %s: Repo %q hooks request: %v", d.name, repoName, err)
		}

		// We need to have just one webhook per repo - all the other hooks will be skipped
		for _, hook := range hooks {
			logger.Debug("Using only one first hook for repo", "repo", repoName, "hook_id", hook.GetID())
			updatedHooks = append(updatedHooks, hook)
			break
		}
	}

	// Updating hooks cache list
	d.hooksMutex.RLock()
	defer d.hooksMutex.RUnlock()

	// To not waste time enabling it only in debug mode
	if log.GetLevel() == log.LevelDebug {
		// Comparing the lists to show the differences
		for _, newHook := range updatedHooks {
			found := false
			for _, oldHook := range d.hooks {
				// To be sure comparing full URL's as well
				if oldHook.GetID() == newHook.GetID() && oldHook.GetURL() == newHook.GetURL() {
					found = true
					break
				}
			}
			if !found {
				logger.Debug("Found new webhook", "hook_url", newHook.GetURL())
			}
		}
		for _, oldHook := range d.hooks {
			found := false
			for _, newHook := range updatedHooks {
				// To be sure comparing full URL's as well
				if oldHook.GetID() == newHook.GetID() && oldHook.GetURL() == newHook.GetURL() {
					found = true
					break
				}
			}
			if !found {
				logger.Debug("Removed known webhook", "hook_url", oldHook.GetURL())
			}
		}
	}

	d.hooks = updatedHooks

	return nil
}

// cleanupRunners is called to make sure there is no stale not connected runners records in github
// It's actually needed, because it's up to the image to register as runner, so alot could go wrong
// with that (like restart of service) and we need to identify such issues as soon as possible.
// Principle - it gets the list of fish runners and stores the offline ephemeral ones till the next
// run. Then next time it checks if the same runners are offline and removes them.
func (d *Driver) cleanupRunners() (outerr error) {
	logger := log.WithFunc("github", "cleanupRunners").With("gate.name", d.name)
	d.hooksMutex.RLock()
	defer d.hooksMutex.RUnlock()

	logger.Debug("Cleanup runners...")
	defer logger.Debug("Cleanup runners done")

	var foundRunners []string
	for _, hook := range d.hooks {
		logger.Debug("cleanupRunners: Processing hook", "hook_url", hook.GetURL())
		// Getting repo name from the webhook URL
		spl := strings.Split(hook.GetURL(), "/")
		if len(spl) < 8 {
			logger.Error("cleanupRunners: Not enough parameters in webhook URL", "hook_url", hook.GetURL())
			outerr = fmt.Errorf("GITHUB: %s: cleanupRunners: Not enough parameters in webhook URL: %s", d.name, hook.GetURL())
			continue
		}
		owner := spl[len(spl)-4]
		repo := spl[len(spl)-3]
		repoName := fmt.Sprintf("%s/%s", owner, repo)

		runners, err := d.apiGetFishEphemeralRunnersList(owner, repo)
		if err != nil {
			logger.Error("Repo runners list request", "repo", repoName, "err", err)
			outerr = fmt.Errorf("GITHUB: %s: Repo %q runners list request: %v", d.name, repoName, err)
		}
		if len(runners) == 0 {
			logger.Debug("cleanupRunners: No fish runners in repo", "repo", repoName)
			continue
		}

		for _, runner := range runners {
			// Skipping all non-offline runners
			if runner.GetStatus() != "offline" {
				continue
			}

			// Checking if the runner is in the naughty list
			// Using unique name & ID, because just ID can be reused
			runnerID := fmt.Sprintf("%s/%s/runner/%s ID:%d", owner, repo, runner.GetName(), runner.GetID())
			found := -1
			for index, id := range d.runnersNaughtyList {
				if id != runnerID {
					continue
				}

				found = index
				break
			}
			if found < 0 {
				// Since not found in the naughty list - adding there, so will be checked next time
				logger.Debug("cleanupRunners: Found offline fish node, adding to list", "runner_id", runnerID)
				foundRunners = append(foundRunners, runnerID)
				continue
			}

			var labels []string
			for _, lbl := range runner.Labels {
				labels = append(labels, lbl.GetName())
			}
			logger.Warn("cleanupRunners: Removing runner, please check what's wrong with the used image", "runner_id", runnerID, "labels", labels)

			// Attempting removing of the runner
			if err := d.apiRemoveRunner(owner, repo, runner.GetID()); err != nil {
				// Ok will try the next time
				foundRunners = append(foundRunners, runnerID)
				logger.Error("cleanupRunners: Unable to remove runner", "runner_id", runnerID)
				outerr = fmt.Errorf("GITHUB: %s: cleanupRunners: Unable to remove runner %q", d.name, runnerID)
				continue
			}

			// Removing runner from naughty list
			d.runnersNaughtyList = append(d.runnersNaughtyList[:found], d.runnersNaughtyList[found+1:]...)
		}
	}

	d.runnersNaughtyList = foundRunners

	return outerr
}

// pullBackgroundProcess starts in background if API Pull is enabled to run tasks periodically
func (d *Driver) pullBackgroundProcess() {
	logger := log.WithFunc("github", "pullBackgroundProcess").With("gate.name", d.name)
	d.routinesMutex.Lock()
	d.routines.Add(1)
	d.routinesMutex.Unlock()
	defer d.routines.Done()
	defer logger.Info("backgroundProcess stopped")

	interval := time.Duration(d.cfg.APIUpdateHooksInterval)
	var updateHooksTicker *time.Ticker
	if interval > 0 {
		updateHooksTicker = time.NewTicker(interval)
		defer updateHooksTicker.Stop()
		logger.Info("backgroundProcess: Triggering updateHooks once per", "interval", interval)
	}

	interval = time.Duration(d.cfg.APICleanupRunnersInterval)
	var cleanupRunnersTicker *time.Ticker
	if interval > 0 {
		cleanupRunnersTicker = time.NewTicker(interval)
		defer cleanupRunnersTicker.Stop()
		logger.Info("backgroundProcess: Triggering cleanupRunners once per", "interval", interval)
	}

	interval = time.Duration(d.cfg.APIMinCheckInterval)
	checkDeliveriesTicker := time.NewTicker(interval)
	defer checkDeliveriesTicker.Stop()
	logger.Info("backgroundProcess: Triggering checkDeliveries once per", "interval", interval)

	for {
		select {
		case <-d.running.Done():
			return
		case <-updateHooksTicker.C:
			d.updateHooks()
		case <-checkDeliveriesTicker.C:
			d.checkDeliveries()
			// TODO: recalculate the interval of checkDeliveriesTicker according to measured Rate
			// TODO: Make sure the new update time is less then d.cfg.DeliveryValidInterval
		case <-cleanupRunnersTicker.C:
			d.cleanupRunners()
		}
	}
}
