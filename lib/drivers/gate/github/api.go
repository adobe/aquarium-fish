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
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"slices"
	"time"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v71/github"

	"github.com/adobe/aquarium-fish/lib/log"
)

// lockClient locks client to be sure no parallel operations will be executed to conform with
// github.com recommendations on using REST APIand repeats client creation until it's connected
func (d *Driver) lockClient() {
	d.clMutex.Lock()
	// TODO: Sleep till time if limits were busted
	var err error
	for d.cl == nil {
		d.cl, err = d.createClient()
		if err != nil {
			log.Errorf("GITHUB: %s: Unable to create github client (waiting for 30s): %v", d.name, err)
			time.Sleep(30 * time.Second)
		}
	}
}

// createClient returns a client based on the provided gate configuration
func (d *Driver) createClient() (client *github.Client, err error) {
	// App auth in priority as superior to token one
	if d.cfg.isAppAuth() {
		// Using GitHub App auth
		itr, err := ghinstallation.New(d.tr, d.cfg.APIAppID, d.cfg.APIAppInstallID, []byte(d.cfg.APIAppKey))
		if err != nil {
			return nil, err
		}

		client = github.NewClient(&http.Client{Transport: itr})
	} else if d.cfg.isTokenAuth() {
		// Using Fine-grained token access
		client = github.NewClient(nil).WithAuthToken(d.cfg.APIToken)
	} else {
		return nil, log.Errorf("GITHUB: %s: No auth is available", d.name)
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

func (d *Driver) apiCheckResponse(resp *github.Response, err error) error {
	if resp != nil {
		d.apiRateMutex.Lock()
		d.apiRate = resp.Rate
		d.apiRateMutex.Unlock()
		log.Debugf("GITHUB: %s: Resp rate: lim:%d, rem:%d, rst:%s", d.name, resp.Rate.Limit, resp.Rate.Remaining, resp.Rate.Reset)
	}

	// Check errors and response to get the data off it
	if err != nil {
		if _, ok := err.(*github.RateLimitError); ok {
			// TODO: Special processing to delay next API request till cutoff time
			return log.Errorf("GITHUB: %s: Hit rate limit", d.name)
		}
		if _, ok := err.(*github.AbuseRateLimitError); ok {
			// TODO: Special processing to delay next API request to reduce pressure
			return log.Errorf("GITHUB: %s: Hit secondary rate limit", d.name)
		}
		return log.Errorf("GITHUB: %s: Request: %v", d.name, err)
	}

	return nil
}

// Will return a list of repos through API depends on what kind of auth you using and filter them
func (d *Driver) apiGetRepos() (repos []string, err error) {
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
			d.clMutex.Unlock()

			if listRepos != nil {
				respRepos = listRepos.Repositories
			}
		} else if d.cfg.isTokenAuth() {
			// In case Token auth is active
			d.lockClient()
			respRepos, resp, err = d.cl.Repositories.ListByAuthenticatedUser(context.Background(), &opts2)
			d.clMutex.Unlock()
		}

		allRepos = append(allRepos, respRepos...)

		if err = d.apiCheckResponse(resp, err); err != nil {
			log.Errorf("GITHUB: %s: Receiving repos list: %v", d.name, err)
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
		log.Debugf("GITHUB: %s: Located repos: %s", d.name, repos)
	}

	return repos, err
}

// Will return a list of IDs of active webhooks in the repository
func (d *Driver) apiGetHooks(owner, repo string) (hooks []*github.Hook, err error) {
	opts := github.ListOptions{PerPage: d.cfg.APIPerPage}

	for {
		d.lockClient()
		respHooks, resp, respErr := d.cl.Repositories.ListHooks(context.Background(), owner, repo, &opts)
		d.clMutex.Unlock()
		if err = d.apiCheckResponse(resp, respErr); err != nil {
			break
		}

		for _, hook := range respHooks {
			// Ensure the webhook is active and has "workflow_job" in the events list
			if hook.GetActive() && slices.Contains(hook.Events, "workflow_job") {
				// Make sure URL is set for the hook - otherwise it will be hard to use while check
				if hook.URL == nil {
					log.Warnf("GITHUB: %s: Found null URL hook in repo %q: %d", d.name, repo, hook.GetID())
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
		d.clMutex.Unlock()
		if err = d.apiCheckResponse(resp, respErr); err != nil {
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
	d.clMutex.Unlock()
	if err = d.apiCheckResponse(resp, err); err != nil {
		return nil, err
	}

	return respDelivery, err
}

// apiCreateRunnerToken will return registration token to allow the worker to connect as runner
func (d *Driver) apiCreateRunnerToken(owner, repo string) (*github.RegistrationToken, error) {
	d.lockClient()
	respRegToken, resp, err := d.cl.Actions.CreateRegistrationToken(context.Background(), owner, repo)
	d.clMutex.Unlock()
	if err = d.apiCheckResponse(resp, err); err != nil {
		return nil, err
	}

	return respRegToken, nil
}

// Will return the actual body of the delivery
/*func (d *Driver) apiCreateRunner(owner, repo string, labels []string) (*github.JITRunnerConfig, error) {
	d.lockClient()
	req := github.GenerateJITConfigRequest{
		Name:   fmt.Sprintf("fish-%s", crypt.RandString(8)),
		Labels: labels,

		RunnerGroupID: 1,
		WorkFolder:    github.Ptr("."),
	}
	respRunnerCfg, resp, err := d.cl.Actions.GenerateRepoJITConfig(context.Background(), owner, repo, &req)
	d.clMutex.Unlock()
	if err = d.apiCheckResponse(resp, err); err != nil {
		return nil, err
	}

	return respRunnerCfg, nil
}*/
