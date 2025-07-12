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
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/google/go-github/v71/github"
	"github.com/google/uuid"

	"github.com/adobe/aquarium-fish/lib/crypt"
	"github.com/adobe/aquarium-fish/lib/database"
	"github.com/adobe/aquarium-fish/lib/log"
	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/util"
)

const (
	// Constants to keep records in DB
	dbPrefixHook = "gate_github_hook"
	dbPrefixJob  = "gate_github_job"

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
	CreatedAt time.Time       `json:"created_at"` // To figure out the cleanup time
	NodeUID   typesv2.NodeUID `json:"node_uid"`   // Which node received the webhook
}

// dbJob is created with dbPrefixJob:RunID-JobID and shows the current status of the job
type dbJob struct {
	CreatedAt   time.Time       `json:"created_at"`  // To figure out the cleanup time
	Status      string          `json:"status"`      // Current job status
	NodeUID     typesv2.NodeUID `json:"node_uid"`    // Which node processing the job
	Description string          `json:"description"` // Simple one-line description of what's up

	ApplicationUID typesv2.ApplicationUID `json:"application_uid"` // Link to the Application
	RunnerID       int64                  `json:"runner_id"`       // Identifies used runner
}

// isWebhookProcessed makes sure there is no duplication in webhooks processing
// It's a quick check because all the DB keys are stored in memory
func (d *Driver) isWebhookProcessed(guid string) bool {
	logger := log.WithFunc("github", "isWebhookProcessed").With("gate.name", d.name)
	if ok, err := d.db.Has(dbPrefixHook, guid); ok {
		//logger.Debug("Skipping processing of duplicated webhook request", "hook_guid", guid)
		return true
	} else if err != nil {
		logger.Error("Unable to check availability of the delivery in DB", "err", err)
		return true
	}
	return false
}

// extractJob extracts job from webhook headers and body
func (d *Driver) extractJob(req *github.HookRequest, failOnSecretUnset bool) (*github.WorkflowJob, error) {
	if req == nil {
		return nil, fmt.Errorf("Request is empty")
	}
	logger := log.WithFunc("github", "extractJob").With("gate.name", d.name)

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
		d.db.Set(dbPrefixHook, guid.String(),
			&dbWebhook{CreatedAt: time.Now(), NodeUID: d.db.GetNodeUID()},
		)
	}
	d.webhooksMutex.Unlock()

	logger.Debug("Extracting new job from webhook", "hook_guid", guid)

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
	logger.Debug("Extracted the job for webrequest", "hook_guid", guid, "job", github.Stringify(workflowJobEvent.GetWorkflowJob()))

	return workflowJobEvent.GetWorkflowJob(), nil
}

// executeJob is in charge of actual action over the received webhook job, so here magic is happening
func (d *Driver) executeJob(owner, repo string, job *github.WorkflowJob) error {
	runJobID := fmt.Sprintf("%d-%d", job.GetRunID(), job.GetID())
	logger := log.WithFunc("github", "executeJob").With("gate.name", d.name, "job_id", runJobID, "repo", owner+"/"+repo)
	logger.Debug("Executing the job", "job_status", job.GetStatus())

	// Let's find the job in DB or create it if action "queue"
	record := dbJob{}
	err := d.db.Get(dbPrefixJob, runJobID, &record)

	// Processing queued event
	if err == database.ErrObjectNotFound && job.GetStatus() == jobQueued {
		// Checking labels on the job to link the right one
		if len(job.Labels) < 2 || job.Labels[0] != "self-hosted" {
			logger.Info("Skipping the job due to incorrect labels provided", "labels", job.Labels)
			// We returning nil here because it's not Fish fault someone made a mistake in workflow
			return nil
		}
		name := job.Labels[1]
		version := "last"
		params := database.LabelListParams{
			Name:    &name,
			Version: &version,
		}
		if strings.Contains(name, ":") {
			spl := strings.SplitN(name, ":", 2)
			params.Name = &spl[0]
			params.Version = &spl[1]
		}
		labels, err := d.db.LabelList(context.Background(), params)
		if err != nil || len(labels) < 1 {
			logger.Info("Skipping the job: Unable to find the requested label", "label", job.Labels[1])
			// We returning nil here because it's not Fish fault someone made a mistake in workflow
			return nil //nolint:nilerr
		}

		// Unfortunately JIT runners has quite tight timeouts on connection (<5min) and has builtin
		// configuration right in JIT token that prevents proper configuration on the image. So
		// here we using self-registering runners with 1h tokens instead.
		runnerToken, err := d.apiCreateRunnerToken(owner, repo)
		if err != nil {
			return fmt.Errorf("Unable to create runner config: %v", err)
		}
		fishName := fmt.Sprintf("fish-%s", crypt.RandString(8))
		logger.Debug("Created runner token for Fish", "fish_id", fishName, "token", runnerToken.GetToken())

		// Sending allocation request to the Fish core to write down the ApplicationUID
		logger.Debug("Creating Application using Label", "label_uid", labels[0].Uid)
		metadata, err := json.Marshal(map[string]string{
			"GITHUB_RUNNER_URL":       fmt.Sprintf("%s/%s/%s", d.githubURL, owner, repo),
			"GITHUB_RUNNER_NAME":      fishName,
			"GITHUB_RUNNER_LABELS":    strings.Join(job.Labels[:2], ","), // Using just first 2 validated labels
			"GITHUB_RUNNER_REG_TOKEN": runnerToken.GetToken(),
		})
		if err != nil {
			return fmt.Errorf("Unable to create application metadata: %v", err)
		}
		app := typesv2.Application{
			LabelUid:  labels[0].Uid,
			OwnerName: d.name,
			Metadata:  util.UnparsedJSON(metadata),
		}
		if err := d.db.ApplicationCreate(context.Background(), &app); err != nil {
			return fmt.Errorf("Unable to create Application: %v", err)
		}

		// Record not found and it's queued - so first time here, need to create one and allocate
		j := dbJob{
			CreatedAt:   time.Now(),
			Status:      job.GetStatus(),
			NodeUID:     d.db.GetNodeUID(),
			Description: fmt.Sprintf("Created by node %s", d.db.GetNodeName()),

			ApplicationUID: app.Uid,
		}
		if err := d.db.Set(dbPrefixJob, runJobID, &j); err != nil {
			return fmt.Errorf("Unable to create db entry for job %d-%d: %v", job.GetRunID(), job.GetID(), err)
		}

		// TODO: probably here we need a monitor that ensure the node was allocated properly
		// It will need to make sure the Application is Allocated and then delay till timeout,
		// Application state change - which should trigger deallocate/create new Application if the
		// job still waits for the runner or dbJob update to in_progress, where monitoring stops.
		return nil
	}

	// Processing in_progress job
	if err == nil && job.GetStatus() == jobInProgress {
		// Ok the node is connected and workload started execution
		// The node could be taken from a different webhook, because there is (sadly) no pinning

		logger.Info("The runner was allocated and executing job", "runner_name", job.GetRunnerName(), "runner_id", job.GetRunnerID())

		// Updating the record in database to reflect the successful allocation
		j := dbJob{
			CreatedAt:   time.Now(),
			Status:      job.GetStatus(),
			NodeUID:     record.NodeUID,
			Description: fmt.Sprintf("Created by node %s", d.db.GetNodeName()),

			ApplicationUID: record.ApplicationUID,
			RunnerID:       job.GetRunnerID(),
		}
		if err := d.db.Set(dbPrefixJob, runJobID, &j); err != nil {
			return fmt.Errorf("Unable to create db entry for job %d-%d: %v", job.GetRunID(), job.GetID(), err)
		}

		return nil
	}

	// Processing completed job
	if err == nil && job.GetStatus() == jobCompleted {
		// Job completed, so it's time to deallocate the worker and make sure no residue is left
		logger.Info("Job is completed, runner should be gone", "job_conclusion", job.GetConclusion(), "runner_id", record.RunnerID)

		// Requesting deallocate of the Application
		if _, err := d.db.ApplicationDeallocate(context.Background(), record.ApplicationUID, fmt.Sprintf("gate/%s", d.name)); err != nil {
			return err
		}

		// Updating the record in database to reflect the successful deallocation
		j := dbJob{
			CreatedAt:   time.Now(),
			Status:      job.GetStatus(),
			NodeUID:     record.NodeUID,
			Description: fmt.Sprintf("Created by node %s", d.db.GetNodeName()),

			ApplicationUID: record.ApplicationUID,
			RunnerID:       record.RunnerID,
		}
		if err := d.db.Set(dbPrefixJob, runJobID, &j); err != nil {
			return fmt.Errorf("Unable to create db entry for job %d-%d: %v", job.GetRunID(), job.GetID(), err)
		}

		return nil
	}

	logger.Debug("Job was skipped: doesn't fit the regular workflow", "job_status", job.GetStatus(), "err", err)

	return nil
}

// cleanupDBProcess makes sure github data in DB stays not for long - but just for the time needed
// It's a separated process to keep it for both webhooks & API.
func (d *Driver) cleanupDBProcess() {
	logger := log.WithFunc("github", "cleanupDBProcess").With("gate.name", d.name)
	d.routinesMutex.Lock()
	d.routines.Add(1)
	d.routinesMutex.Unlock()
	defer d.routines.Done()
	defer logger.Info("cleanupDBProcess stopped")

	interval := time.Duration(d.cfg.DeliveryValidInterval)
	cleanupTicker := time.NewTicker(interval)
	defer cleanupTicker.Stop()
	logger.Info("cleanupDBProcess: Triggering cleanupDB once per", "interval", interval)

	for {
		select {
		case <-d.running.Done():
			return
		case <-cleanupTicker.C:
			d.cleanupDB()
		}
	}
}

// cleanupDB cleans the outdated hooks & jobs
func (d *Driver) cleanupDB() {
	logger := log.WithFunc("github", "cleanupDB").With("gate.name", d.name)
	// Counters to keep statistics
	counterFound := 0
	counterRemoved := 0
	var counterMutex sync.Mutex

	// With hooks it's relatively easy - we don't need hook when it's over DeliveryValidInterval
	// Those are used to not process the webrequest with unique GUID twice
	deliveryCutTime := time.Now().Add(-time.Duration(d.cfg.DeliveryValidInterval))
	d.db.Scan(dbPrefixHook, func(key string) error {
		counterMutex.Lock()
		counterFound++
		counterMutex.Unlock()
		var hook dbWebhook
		d.db.Get(dbPrefixHook, key, &hook)
		if hook.CreatedAt.Before(deliveryCutTime) {
			logger.Debug("Cleaning webhook record", "record", dbPrefixHook+":"+key)
			d.db.Del(dbPrefixHook, key)
			counterMutex.Lock()
			counterRemoved++
			counterMutex.Unlock()
		}
		return nil
	})

	// With jobs it's more complicated - we need to keep them at least until the job is completed
	// but it's also possible they are stuck, in that case we give it MaxJobLifetime and remove
	defaultStaleCutTime := time.Now().Add(-time.Duration(d.cfg.DefaultJobMaxLifetime))
	d.db.Scan(dbPrefixJob, func(key string) error {
		counterMutex.Lock()
		counterFound++
		counterMutex.Unlock()
		var job dbJob
		d.db.Get(dbPrefixJob, key, &job)
		// Initial filter to keep records at least for the mentioned time
		if job.CreatedAt.Before(deliveryCutTime) {
			switch job.Status {
			case jobCompleted:
				// The easiest case is with completed jobs - means the application is deallocated
				logger.Debug("Cleaning job record", "job_status", job.Status, "record", dbPrefixJob+":"+key)
				if err := d.db.Del(dbPrefixJob, key); err != nil {
					logger.Error("Cleaning job record failed", "job_status", job.Status, "record", dbPrefixJob+":"+key, "err", err)
				} else {
					counterMutex.Lock()
					counterRemoved++
					counterMutex.Unlock()
				}
			case jobQueued:
				// Job can stuck in queue for a number of reasons, but it will always be updated by
				// the monitoring to kep the gears rolling. In case it's stall - we will clean it
				// in DefaultJobMaxLifetime
				if job.CreatedAt.Before(defaultStaleCutTime) {
					logger.Warn("Forcefully removing stale job", "job_status", job.Status, "key", key, "app_uid", job.ApplicationUID)

					// Requesting deallocate of the Application
					if _, err := d.db.ApplicationDeallocate(context.Background(), job.ApplicationUID, fmt.Sprintf("gate/%s", d.name)); err != nil {
						logger.Error("Unable to deallocate Application for job", "app_uid", job.ApplicationUID, "key", key, "err", err)
						// Will try next time
						return nil
					}
					if err := d.db.Del(dbPrefixJob, key); err != nil {
						logger.Error("Cleaning job record failed", "job_status", job.Status, "record", dbPrefixJob+":"+key, "err", err)
					} else {
						counterMutex.Lock()
						counterRemoved++
						counterMutex.Unlock()
					}
				}
			case jobInProgress:
				// In theory those are in progress, so should be concluded by cancelling or
				// completing, otherwise we just wait till Application lifetime (+deliveryCutTime) is over (or
				// DefaultJobMaxLifetime), and then removing and deallocating the Application as well

				// First check if the app resource is here at all
				appRes, err := d.db.ApplicationResourceGetByApplication(context.Background(), job.ApplicationUID)
				if err != nil {
					logger.Warn("Forcefully removing stale job", "job_status", job.Status, "key", key, "app_uid", job.ApplicationUID, "err", err)
					if err := d.db.Del(dbPrefixJob, key); err != nil {
						logger.Error("Cleaning job record failed", "job_status", job.Status, "record", dbPrefixJob+":"+key, "err", err)
					} else {
						counterMutex.Lock()
						counterRemoved++
						counterMutex.Unlock()
					}
					return nil
				}

				// Next if the app is not allocated
				appState, err := d.db.ApplicationStateGetByApplication(context.Background(), job.ApplicationUID)
				if err != nil || !d.db.ApplicationStateIsActive(appState.Status) {
					logger.Warn("Forcefully removing stale job", "job_status", job.Status, "key", key, "app_uid", job.ApplicationUID, "err", err)
					if err := d.db.Del(dbPrefixJob, key); err != nil {
						logger.Error("Cleaning job record failed", "job_status", job.Status, "record", dbPrefixJob+":"+key, "err", err)
					} else {
						counterMutex.Lock()
						counterRemoved++
						counterMutex.Unlock()
					}
					return nil
				}

				// The app seems allocated - but maybe it's time to cut the losses
				var appTimeout time.Time
				if appRes.Timeout != nil {
					appTimeout = *appRes.Timeout
				}
				if appTimeout.IsZero() {
					// Seems resource have no timeout - so using default as last resort
					appTimeout = appRes.CreatedAt.Add(time.Duration(d.cfg.DefaultJobMaxLifetime))
				}

				if appTimeout.Before(time.Now()) {
					logger.Warn("Forcefully removing stale job", "job_status", job.Status, "key", key, "app_uid", job.ApplicationUID)

					// Requesting deallocate of the Application
					if _, err := d.db.ApplicationDeallocate(context.Background(), job.ApplicationUID, fmt.Sprintf("gate/%s", d.name)); err != nil {
						logger.Error("Unable to deallocate Application for job", "app_uid", job.ApplicationUID, "key", key, "err", err)
						// Will try next time
						return nil
					}
					if err := d.db.Del(dbPrefixJob, key); err != nil {
						logger.Error("Cleaning job record failed", "job_status", job.Status, "record", dbPrefixJob+":"+key, "err", err)
					} else {
						counterMutex.Lock()
						counterRemoved++
						counterMutex.Unlock()
					}
				}
			}
		}
		return nil
	})

	logger.Debug("Found records and cleaned", "records", counterFound, "cleaned", counterRemoved)
}

// Init for token
func (d *Driver) init() error {
	logger := log.WithFunc("github", "init").With("gate.name", d.name)
	// Starting webhook listener first to quickly recover after restart
	if d.cfg.isWebhookEnabled() {
		// TODO: Listen for webhook
		// It's easy to do - just listen for post requests on BindAddress from config and send to
		// existing procesing function d.extractJob and then to d.executeJob
		logger.Warn("WebHook listener not yet implemented")
	}

	// Now running relatively slow API repo updater to ensure the creds are working correctly
	if d.cfg.isAPIEnabled() {
		// Validating client
		var err error
		if d.cl, err = d.createClient(); err != nil {
			logger.Error("Failed to create client", "err", err)
			return fmt.Errorf("GITHUB: %s: Failed to create client: %v", d.name, err)
		}

		// Receiving hooks from github - checking if the API connectivity works correctly
		if err = d.updateHooks(); err != nil {
			logger.Error("Failed to update the repositories list", "err", err)
			return fmt.Errorf("GITHUB: %s: Failed to update the repositories list: %v", d.name, err)
		}

		// Checking for the stale runners
		if err := d.cleanupRunners(); err != nil {
			logger.Error("Failed to check stale runners", "err", err)
			return fmt.Errorf("GITHUB: %s: Failed to check stale runners: %v", d.name, err)
		}

		// Checking if there is new deliveries
		if err := d.checkDeliveries(); err != nil {
			logger.Error("Failed to check deliveries", "err", err)
			return fmt.Errorf("GITHUB: %s: Failed to check deliveries: %v", d.name, err)
		}

		// Run schedule to update deliveries periodically
		go d.pullBackgroundProcess()
	}

	go d.cleanupDBProcess()

	return nil
}
