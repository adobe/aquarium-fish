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
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v71/github"

	"github.com/adobe/aquarium-fish/lib/log"
)

// lockClient locks client to be sure no parallel operations will be executed to conform with
// github.com recommendations on using REST APIand repeats client creation until it's connected
func (d *Driver) lockClient() {
	logger := log.WithFunc("github", "lockClient").With("gate.name", d.name)
	d.clMutex.Lock()

	// In case REST API requested to back off for a bit
	for time.Now().Before(d.clDelayTill) {
		toSleep := time.Until(d.clDelayTill)
		logger.Warn("REST API operations suspended", "sleep_duration", toSleep)
		if toSleep > 31*time.Second {
			toSleep = 30 * time.Second
		}
		time.Sleep(toSleep)
	}

	var err error
	for d.cl == nil {
		d.cl, err = d.createClient()
		if err != nil {
			logger.Error("Unable to create github client (waiting for 30s)", "err", err)
			time.Sleep(30 * time.Second)
		}
	}
}

// createClient returns a client based on the provided gate configuration
func (d *Driver) createClient() (client *github.Client, err error) {
	logger := log.WithFunc("github", "createClient").With("gate.name", d.name)
	logger.Debug("Creating new client")
	// App auth in priority as superior to token one
	if d.cfg.isAppAuth() {
		// Creating our own transport to recover on failure - DefaultTransport is quite hard to
		// reset if the things goes sideways (like 403 due to connection error).
		dialer := &net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}
		tr := http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           dialer.DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
		// Using GitHub App auth
		itr, err := ghinstallation.New(&tr, d.cfg.APIAppID, d.cfg.APIAppInstallID, []byte(d.cfg.APIAppKey))
		if err != nil {
			return nil, err
		}

		client = github.NewClient(&http.Client{Transport: itr})
	} else if d.cfg.isTokenAuth() {
		// Using Fine-grained token access
		client = github.NewClient(nil).WithAuthToken(d.cfg.APIToken)
	} else {
		logger.Error("No auth is available")
		return nil, fmt.Errorf("GITHUB: %s: No auth is available", d.name)
	}

	if d.cfg.EnterpriseBaseURL != "" && d.cfg.EnterpriseUploadURL != "" {
		parsedURL, err := url.Parse(d.cfg.EnterpriseBaseURL)
		if err != nil {
			return nil, fmt.Errorf("Unable to parse EnterpriseBaseURL: %v", err)
		}
		client.BaseURL = parsedURL

		parsedURL, err = url.Parse(d.cfg.EnterpriseUploadURL)
		if err != nil {
			return nil, fmt.Errorf("Unable to parse EnterpriseUploadURL: %v", err)
		}
		client.UploadURL = parsedURL
	}

	return client, nil
}

// apiCheckResponse makes sure response is ok
// WARNING: the client should be already locked by the function clientLock
func (d *Driver) apiCheckResponse(resp *github.Response, err error) error {
	logger := log.WithFunc("github", "apiCheckResponse").With("gate.name", d.name)
	if resp != nil {
		d.apiRateMutex.Lock()
		d.apiRate = resp.Rate
		d.apiRateMutex.Unlock()
		logger.Debug("Resp rate", "lim", resp.Rate.Limit, "rem", resp.Rate.Remaining, "rst", resp.Rate.Reset)
	}

	// Check errors and response to get the data off it
	if err != nil {
		if _, ok := err.(*github.AbuseRateLimitError); ok {
			// Since we hit secondary rate limit - wait a minute for the next request
			d.clDelayTill = time.Now().Add(time.Minute)
			logger.Error("Hit REST API frequency rate limit, delay next request by 1m")
		}
		if _, ok := err.(*github.RateLimitError); ok {
			// Since we hit the rate limit - waiting until the next reset + 30 seconds in case time is off
			d.clDelayTill = resp.Rate.Reset.Add(30 * time.Second)
			logger.Error("Hit REST API rate limit, delay next request till next reset", "delay_till", time.Until(d.clDelayTill))
		}

		logger.Debug("Resetting client")
		d.cl = nil
		logger.Error("Request", "err", err)
		return fmt.Errorf("GITHUB: %s: Request: %v", d.name, err)
	}

	return nil
}

// Will return a list of repos through API depends on what kind of auth you using and filter them
func (d *Driver) apiGetRepos() (repos []string, err error) {
	logger := log.WithFunc("github", "apiGetRepos").With("gate.name", d.name)
	opts := github.ListOptions{PerPage: d.cfg.APIPerPage}
	opts2 := github.RepositoryListByAuthenticatedUserOptions{
		ListOptions: opts,
	}

	var allRepos []*github.Repository
	for {
		var resp *github.Response
		var respRepos []*github.Repository
		if d.cfg.isAppAuth() {
			// In case App auth is active (priority over token auth)
			var listRepos *github.ListRepositories

			d.lockClient()
			listRepos, resp, err = d.cl.Apps.ListRepos(context.Background(), &opts)

			if listRepos != nil {
				respRepos = listRepos.Repositories
			}
		} else if d.cfg.isTokenAuth() {
			// In case Token auth is active
			d.lockClient()
			respRepos, resp, err = d.cl.Repositories.ListByAuthenticatedUser(context.Background(), &opts2)
		} else {
			return repos, fmt.Errorf("No auth is set")
		}

		allRepos = append(allRepos, respRepos...)

		err = d.apiCheckResponse(resp, err)
		d.clMutex.Unlock()
		if err != nil {
			logger.Error("Failed to receive repos list", "err", err)
			break
		}

		opts.Page = resp.NextPage
		opts2.Page = resp.NextPage

		if resp.NextPage == 0 {
			break
		}
	}

	// Filtering returned repos according to patterns we have to prevent uncontrolled access
	for _, repo := range allRepos {
		repoName := repo.GetFullName()

		// Filtering with repo filters
		match := false
		for pattern := range d.cfg.Filters {
			if ok, _ := path.Match(pattern, repoName); ok {
				match = true
				break
			}
		}
		if match {
			repos = append(repos, repoName)
		}
	}
	if len(repos) > 0 {
		logger.Debug("Located repos", "repos", repos)
	}

	return repos, err
}

// Will return a list of IDs of active webhooks in the repository
func (d *Driver) apiGetHooks(owner, repo string) (hooks []*github.Hook, err error) {
	logger := log.WithFunc("github", "apiGetHooks").With("gate.name", d.name)
	opts := github.ListOptions{PerPage: d.cfg.APIPerPage}

	for {
		d.lockClient()
		respHooks, resp, respErr := d.cl.Repositories.ListHooks(context.Background(), owner, repo, &opts)
		err = d.apiCheckResponse(resp, respErr)
		d.clMutex.Unlock()
		if err != nil {
			break
		}

		for _, hook := range respHooks {
			// Ensure the webhook is active and has "workflow_job" in the events list
			if hook.GetActive() && slices.Contains(hook.Events, "workflow_job") {
				// Make sure URL is set for the hook - otherwise it will be hard to use while check
				if hook.URL == nil {
					logger.Warn("Found null URL hook in repo", "repo", repo, "hook_id", hook.GetID())
					continue
				}
				hooks = append(hooks, hook)
			}
		}

		opts.Page = resp.NextPage
		if resp.NextPage == 0 {
			break
		}
	}

	return hooks, err
}

// Will return a list of deliveries IDs with no Request/Response body
func (d *Driver) apiGetDeliveriesList(owner, repo string, hook int64) (deliveries []*github.HookDelivery, err error) {
	opts := github.ListCursorOptions{PerPage: d.cfg.APIPerPage}

	for {
		d.lockClient()
		respDeliveries, resp, respErr := d.cl.Repositories.ListHookDeliveries(context.Background(), owner, repo, hook, &opts)
		err = d.apiCheckResponse(resp, respErr)
		d.clMutex.Unlock()
		if err != nil {
			break
		}

		for _, delivery := range respDeliveries {
			// Need to stop if we processed all deliveries to the last checkpoint
			// It prevents us from processing all the available deliveries in the hook
			t := delivery.DeliveredAt.GetTime()
			if d.apiCheckpoint.After(*t) {
				return deliveries, err
			}

			// Check if delivery is older then certain
			// Checking if delivery actually fits our needs
			if !d.validateDelivery(delivery) {
				continue
			}

			// Filtering out job actions we don't care about
			if slices.Contains(jobsToCareAbout, delivery.GetAction()) {
				deliveries = append(deliveries, delivery)
			}
		}

		opts.Page = resp.NextPageToken
		if resp.NextPage == 0 {
			break
		}
	}

	return deliveries, err
}

// apiGetFullDelivery will return the actual body of the delivery
func (d *Driver) apiGetFullDelivery(owner, repo string, hook int64, delivery int64) (*github.HookDelivery, error) {
	d.lockClient()
	respDelivery, resp, err := d.cl.Repositories.GetHookDelivery(context.Background(), owner, repo, hook, delivery)
	err = d.apiCheckResponse(resp, err)
	d.clMutex.Unlock()
	if err != nil {
		return nil, err
	}

	return respDelivery, err
}

// apiCreateRunnerToken will return registration token to allow the worker to connect as runner
func (d *Driver) apiCreateRunnerToken(owner, repo string) (*github.RegistrationToken, error) {
	d.lockClient()
	respRegToken, resp, err := d.cl.Actions.CreateRegistrationToken(context.Background(), owner, repo)
	err = d.apiCheckResponse(resp, err)
	d.clMutex.Unlock()
	if err != nil {
		return nil, err
	}

	return respRegToken, nil
}

// apiGetFishEphemeralRunnersList returns only fish and ephemeral runners attached to repository
func (d *Driver) apiGetFishEphemeralRunnersList(owner, repo string) (runners []*github.Runner, err error) {
	opts := github.ListRunnersOptions{
		ListOptions: github.ListOptions{PerPage: d.cfg.APIPerPage},
	}

	for {
		d.lockClient()
		respRunners, resp, respErr := d.cl.Actions.ListRunners(context.Background(), owner, repo, &opts)
		err = d.apiCheckResponse(resp, respErr)
		d.clMutex.Unlock()
		if err != nil || respRunners == nil {
			break
		}

		for _, runner := range respRunners.Runners {
			// We need only fish ephemeral (TODO: when api will support) nodes
			if strings.HasPrefix(runner.GetName(), "fish-") /*&& runner.GetEphemeral()*/ {
				runners = append(runners, runner)
			}
		}

		opts.Page = resp.NextPage
		if resp.NextPage == 0 {
			break
		}
	}

	return runners, err
}

// apiRemoveRunner removes runner from repository
func (d *Driver) apiRemoveRunner(owner, repo string, runnerID int64) (err error) {
	d.lockClient()
	resp, respErr := d.cl.Actions.RemoveRunner(context.Background(), owner, repo, runnerID)
	err = d.apiCheckResponse(resp, respErr)
	d.clMutex.Unlock()
	if err != nil {
		return err
	}

	return nil
}
