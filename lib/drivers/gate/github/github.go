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

package github

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/google/go-github/v71/github"
	"github.com/google/uuid"

	"github.com/adobe/aquarium-fish/lib/database"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
	"github.com/adobe/aquarium-fish/lib/util"
)

const (
	// Constants to keep records in DB
	dbPrefixHook = "gate_github_hook:"
	dbPrefixJob  = "gate_github_job:"

	// Action/Status of the incoming webhook workflow_job's
	jobQueued     = "queued"      // means we need to "allocate"
	jobInProgress = "in_progress" // is the event to tell that the environment is connected and started
	jobCompleted  = "completed"   // it fires when the job is cancelled or completed, means "deallocate"
	// Another one here is "waiting" - but we don't care for now, because it waits for approval
)

// The job status/action we care about
var jobsToCareAbout = []string{jobQueued, jobInProgress, jobCompleted}

// dbWebhook is created with dbPrefixHook:guid and is here to tell that the webhook was processed
type dbWebhook struct {
	CreatedAt time.Time     `json:"created_at"` // To figure out the cleanup time
	NodeUID   types.NodeUID `json:"node_uid"`   // Which node received the webhook
}

// dbJob is created with dbPrefixJob:RunID-JobID and shows the current status of the job
type dbJob struct {
	CreatedAt   time.Time     `json:"created_at"`  // To figure out the cleanup time
	Status      string        `json:"status"`      // Current job status
	NodeUID     types.NodeUID `json:"node_uid"`    // Which node processing the job
	Description string        `json:"description"` // Simple one-line description of what's up

	ApplicationUID types.ApplicationUID `json:"application_uid"` // Link to the Application
	RunnerID       int64                `json:"runner_id"`       // Identifies used runner
}

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
// It will run on schedule if gate is configured configured for both Push and Pull
func (d *Driver) checkDeliveries() error {
	d.hooksMutex.RLock()
	defer d.hooksMutex.RUnlock()

	log.Debugf("GITHUB: %s: Checking deliveries...", d.name)
	defer log.Debugf("GITHUB: %s: Checking deliveries done", d.name)

	var checkpointUpdate time.Time
	checkpointUpdated := false

	for _, hook := range d.hooks {
		log.Debugf("GITHUB: %s: Processing hook %s", d.name, hook.GetURL())
		// Getting repo name from the webhook URL
		spl := strings.Split(hook.GetURL(), "/")
		if len(spl) < 8 {
			log.Errorf("GITHUB: %s: Not enough parameters in webhook URL: %s", d.name, hook.GetURL())
			continue
		}
		owner := spl[len(spl)-4]
		repo := spl[len(spl)-3]
		repoName := fmt.Sprintf("%s/%s", owner, repo)

		deliveries, err := d.apiGetDeliveriesList(owner, repo, hook.GetID())
		if err != nil {
			log.Errorf("GITHUB: %s: Repo %q hook %d deliveries list request: %v", d.name, repoName, hook.GetID(), err)
		}
		if len(deliveries) == 0 {
			log.Warnf("GITHUB: %s: Repo %q hook %d deliveries list is empty: %d", d.name, repoName, hook.GetID(), len(deliveries))
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
		d.apiCheckpoint = checkpointUpdate
		log.Debugf("GITHUB: %s: Updated deliveries checkpoint to %s", d.name, d.apiCheckpoint)
	}

	return nil
}

// processWebhooks is needed to execute a bunch of webhooks in the right order and remove the
// webhooks that are cancelling each other (when there is "queued" and "completed" webhook)
// This function running in a separated goroutine, so just prints out the result
func (d *Driver) processHookDeliveries(deliveries []*github.HookDelivery, owner, repo string, hookID int64) error {
	var jobs []*github.WorkflowJob
	for _, delivery := range deliveries {
		log.Debugf("GITHUB: %s: Getting full delivery for %s/%s webhook %d: %s", d.name, owner, repo, hookID, delivery.String())

		// Receiving full delivery body
		fullDelivery, err := d.apiGetFullDelivery(owner, repo, hookID, delivery.GetID())
		if err != nil {
			return log.Errorf("GITHUB: %s: Repo %s/%s full delivery %s request: %v", d.name, owner, repo, delivery.GetGUID(), err)
		}

		// Extracting job fom webhook request with optional verification of the secret if
		// it's not set - because we already reading from github.com, so should be enough for
		// proper security measures - if we don't trust github.com, then we have a problem
		if job, err := d.extractJob(fullDelivery.Request, false); err != nil {
			log.Errorf("GITHUB: %s: Error processing repo %s/%s webhook request %s: %v", d.name, owner, repo, delivery.GetGUID(), err)
		} else if job != nil {
			jobs = append(jobs, job)
		}
	}

	// Optimization to filter jobs and cancel-out the completed and queued ones with the same IDs
	var filteredJobs []*github.WorkflowJob
	var completed []*github.WorkflowJob
	for _, job := range jobs {
		if job.GetStatus() == "completed" {
			completed = append(completed, job)
		}
	}
	// We collected completed jobs, now needed to cancel out the jobs in the same id/runid
	// At the same time we going in reverse to execute jobs from oldest to newest
	// The algorithm passes dup completed jobs, but since they were not queued - no issues here
	for i := len(jobs) - 1; i >= 0; i-- {
		job := jobs[i]
		nomatch := true
		for _, compJob := range completed {
			if compJob.GetID() == job.GetID() && compJob.GetRunID() == job.GetRunID() && compJob != job {
				log.Debugf("GITHUB: %s: Cancelling out queued-completed job: %d-%d", d.name, job.GetRunID(), job.GetID())
				nomatch = false
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
			log.Errorf("GITHUB: %s: Error executing job for repo %s/%s run-job %d-%d: %v", d.name, owner, repo, job.GetRunID(), job.GetID(), err)
		}
	}

	return nil
}

// isWebhookProcessed makes sure there is no duplication in webhooks processing
// It's a quick check because all the DB keys are stored in memory
func (d *Driver) isWebhookProcessed(guid string) bool {
	if ok, err := d.db.HasValue(dbPrefixHook, guid); ok {
		//log.Debugf("GITHUB: %s: Skipping processing of duplicated webhook request: %q", d.name, guid)
		return true
	} else if err != nil {
		log.Errorf("GITHUB: %s: Unable to check availability of the delivery in DB: %v", d.name, err)
		return true
	}
	return false
}

// extractJob checks the request and processes it to take the action
func (d *Driver) extractJob(req *github.HookRequest, failOnSecretUnset bool) (*github.WorkflowJob, error) {
	if req == nil {
		return nil, fmt.Errorf("Request is empty")
	}

	headers := req.GetHeaders()

	// github.DeliveryIDHeader is "X-Github-Delivery" so doesn't work.
	// Issue: https://github.com/google/go-github/issues/3555
	guidStr, ok := headers["X-GitHub-Delivery"]
	if !ok {
		return nil, fmt.Errorf("Can't find delivery GUID in headers")
	}
	guid, err := uuid.Parse(guidStr)
	if err != nil {
		return nil, fmt.Errorf("Can't parse delivery GUID from headers")
	}

	// Make sure we don't have it in our database already
	d.webhooksMutex.Lock()
	{
		if d.isWebhookProcessed(guid.String()) {
			d.webhooksMutex.Unlock()
			return nil, nil
		}
		// Storing webhook guid to not process it repeatedly in the future
		d.db.SetValue(dbPrefixHook, guid.String(),
			&dbWebhook{CreatedAt: time.Now(), NodeUID: d.db.GetNodeUID()},
		)
	}
	d.webhooksMutex.Unlock()

	log.Debugf("GITHUB: %s: Extracting new job from webhook: %s", d.name, guid)

	sig, ok := headers[github.SHA256SignatureHeader]
	if !ok {
		return nil, fmt.Errorf("Can't find delivery signature in headers: %#v", headers)
	}
	// github.EventTypeHeader is "X-Github-Event" so doesn't work.
	// Issue: https://github.com/google/go-github/issues/3555
	eventType, ok := headers["X-GitHub-Event"]
	if !ok {
		return nil, fmt.Errorf("Can't find delivery event type in headers")
	}

	// Unfortunately GitHub don't send any repository identificator in the headers, so we have to
	// parse json to find out the repo name to validate the signature of the webhook...
	event, err := github.ParseWebHook(eventType, req.GetRawPayload())
	if err != nil {
		return nil, fmt.Errorf("Unable to parse webhook data: %v", err)
	}
	workflowJobEvent, ok := event.(*github.WorkflowJobEvent)
	if !ok {
		return nil, fmt.Errorf("Unable to convert event to workflow job event")
	}

	repo := workflowJobEvent.GetRepo()
	if repo == nil {
		return nil, fmt.Errorf("Repository is empty in the workflow job event")
	}

	repoName := repo.GetFullName()

	// Check for signature of the request to be sure for sure
	match := false
	valid := false
	for pattern, cfg := range d.cfg.Filters {
		if ok, _ := path.Match(pattern, repoName); ok {
			match = true
			if cfg.WebhookSecret != "" {
				if err := github.ValidateSignature(sig, req.GetRawPayload(), []byte(cfg.WebhookSecret)); err == nil {
					valid = true
					break
				}
			}
		}
	}
	if failOnSecretUnset && valid {
		return nil, fmt.Errorf("Unable to find pattern with secret that would fit the repo: %q", repoName)
	}
	if !match {
		return nil, fmt.Errorf("Repo of the delivery is not in the filter's patterns: %q", repoName)
	}

	// Here we know the webhook is valid so we can return the job back
	log.Debugf("GITHUB: %s: Extracted the job for webrequest %q: %s", d.name, guid, github.Stringify(workflowJobEvent.GetWorkflowJob()))

	return workflowJobEvent.GetWorkflowJob(), nil
}

// executeJob is in charge of actual action over the received webhook job, so here magic is happening
func (d *Driver) executeJob(owner, repo string, job *github.WorkflowJob) error {
	jobID := fmt.Sprintf("%d-%d", job.GetRunID(), job.GetID())
	log.Debugf("GITHUB: %s: Executing the job %s for repo %s/%s: %s %q", d.name, jobID, owner, repo, job.GetStatus(), job.Labels)

	// Let's find the job in DB or create it if action "queue"
	record := dbJob{}
	err := d.db.GetValue(dbPrefixJob, jobID, &record)
	if err == database.ErrObjectNotFound && job.GetStatus() == jobQueued {
		// Checking labels on the job to link the right one
		if job.Labels == nil || len(job.Labels) < 2 || job.Labels[0] != "self-hosted" {
			log.Infof("GITHUB: %s: Skipping the job %s in repo %s/%s due to incorrect labels provided: %q", d.name, jobID, owner, repo, job.Labels)
			return nil
		}
		name := job.Labels[1]
		version := "last"
		params := types.LabelListGetParams{
			Name:    &name,
			Version: &version,
		}
		if strings.Contains(name, ":") {
			spl := strings.SplitN(name, ":", 2)
			params.Name = &spl[0]
			params.Version = &spl[1]
		}
		labels, err := d.db.LabelList(params)
		if err != nil || len(labels) < 1 {
			log.Infof("GITHUB: %s: Skipping the job %s on repo %s/%s: Unable to find the requested label %q", d.name, jobID, owner, repo, job.Labels[1])
			return nil
		}

		// Creating new self-hosted configuration to allow the worker to connect to github
		runnerCfg, err := d.apiCreateRunner(owner, repo, job.Labels)
		if err != nil {
			return fmt.Errorf("Unable to create runner config: %v", err)
		}
		log.Debugf("GITHUB: %s: Job %s of repo %s/%s: Created runner %d %q: %q", d.name, jobID, owner, repo, runnerCfg.Runner.GetID(), runnerCfg.Runner.GetName(), runnerCfg.GetEncodedJITConfig())

		// Sending allocation request to the Fish core to write down the ApplicationUID
		log.Debugf("GITHUB: %s: Job %s of repo %s/%s: Creating Application using Label %q", d.name, jobID, owner, repo, labels[0].UID)
		metadata, err := json.Marshal(map[string]string{
			"GITHUB_URL":       "TODO",
			"GITHUB_JIT_TOKEN": runnerCfg.GetEncodedJITConfig(),
		})
		app := types.Application{
			LabelUID:  labels[0].UID,
			OwnerName: d.name,
			Metadata:  util.UnparsedJSON(metadata),
		}
		if err := d.db.ApplicationCreate(&app); err != nil {
			return fmt.Errorf("Unable to create Application: %v", err)
		}

		// Record not found and it's queued - so first time here, need to create one and allocate
		j := dbJob{
			CreatedAt:   time.Now(),
			Status:      job.GetStatus(),
			NodeUID:     d.db.GetNodeUID(),
			Description: fmt.Sprintf("Created by node %s", d.db.GetNodeName()),

			ApplicationUID: app.UID,
			RunnerID:       runnerCfg.Runner.GetID(),
		}
		if err := d.db.SetValue(dbPrefixJob, jobID, &j); err != nil {
			return fmt.Errorf("Unable to create db entry for job %d-%d: %v", job.GetRunID(), job.GetID(), err)
		}
	}

	// TODO: Run monitoring on successful queue or retry
	return nil
}

// updateHooks is needed to get all the available webhooks from the repos we have
// It will be skipped if Pull by API is not configured
// It will run on schedule if gate is configured only for Pull by API
// It will run only during initialization if gate configured for both Push and Pull
func (d *Driver) updateHooks() error {
	log.Debugf("GITHUB: %s: Updating hooks...", d.name)
	defer log.Debugf("GITHUB: %s: Updating hooks done", d.name)

	repos, err := d.apiGetRepos()
	if err != nil {
		log.Warnf("GITHUB: %s: Unable to get all available repositories: %v", d.name, err)
	}
	if len(repos) == 0 {
		log.Errorf("GITHUB: %s: No available repositories found: %d", d.name, len(repos))
		return nil
	}

	var updatedHooks []*github.Hook

	for _, repoName := range repos {
		spl := strings.SplitN(repoName, "/", 2)
		if len(spl) < 2 {
			log.Errorf("GITHUB: %s: Incorrect repo full name: %q", d.name, repoName)
			continue
		}

		hooks, err := d.apiGetHooks(spl[0], spl[1])
		if err != nil {
			return log.Errorf("GITHUB: %s: Repo %q hooks request: %v", d.name, repoName, err)
		}

		for _, hook := range hooks {
			updatedHooks = append(updatedHooks, hook)
		}
	}

	// Updating hooks cache list
	d.hooksMutex.RLock()
	defer d.hooksMutex.RUnlock()

	// To not waste time enabling it only in debug mode
	if log.GetVerbosity() == log.VerbosityDebug {
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
				log.Debugf("GITHUB: %s: Found new webhook: %s", d.name, newHook.GetURL())
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
				log.Debugf("GITHUB: %s: Removed known webhook: %s", oldHook.GetURL())
			}
		}
	}

	d.hooks = updatedHooks

	return nil
}

// backgroundProcess starts in background if API Pull is enabled to run tasks periodically
func (d *Driver) backgroundProcess() {
	d.routinesMutex.Lock()
	d.routines.Add(1)
	d.routinesMutex.Unlock()
	defer d.routines.Done()
	defer log.Infof("GITHUB: %s: backgroundProcess stopped", d.name)

	updateHooksTicker := time.NewTicker(time.Duration(d.cfg.APIUpdateHooksInterval))
	defer updateHooksTicker.Stop()
	checkDeliveriesTicker := time.NewTicker(time.Duration(d.cfg.APIMinCheckInterval))
	defer checkDeliveriesTicker.Stop()

	// Let's not wait and check the deliveries right away
	d.checkDeliveries()

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
		}
	}
}

// Init for token
func (d *Driver) init() error {
	// Starting webhook listener first to quickly recover after restart
	if d.cfg.isWebhookEnabled() {
		// TODO: Listen for webhook
	}

	// Now running relatively slow API repo updater to ensure the creds are working correctly
	if d.cfg.isAPIEnabled() {
		if err := d.updateHooks(); err != nil {
			return log.Errorf("GITHUB: %s: Failed to update the repositories list:", err)
		}

		// Run schedule to update deliveries periodically
		go d.backgroundProcess()
	}

	// TODO: Start cleanup mechanism for DB items (2x of the current d.cfg.DeliveryValidInterval)

	return nil
}
