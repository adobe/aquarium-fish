/**
 * Copyright 2024 Adobe. All rights reserved.
 * This file is licensed to you under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License. You may obtain a copy
 * of the License at http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
 * OF ANY KIND, either express or implied. See the License for the specific language
 * governing permissions and limitations under the License.
 */

package aws

import (
	"context"
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/adobe/aquarium-fish/lib/log"
)

// HostReserved - custom status to set in the host for simplifying parallel ops in between the updates
const HostReserved = "reserved"

// TODO: Right now logic pinned to just one node, need to be distributed

// This structure keeps the available list of hosts & state to operate on hosts management
type dedicatedPoolWorker struct {
	name   string
	driver *Driver
	record DedicatedPoolRecord

	// Amount of instances per dedicated host used in capacity calculations
	instancesPerHost uint

	// It's better to update activeHosts by calling updateDedicatedHosts()
	activeHosts        map[string]ec2types.Host
	activeHostsUpdated time.Time
	activeHostsMu      sync.RWMutex

	// Storage to delay available state for previously pending state
	pendingAvailableHosts   map[string]time.Time
	pendingAvailableHostsMu sync.Mutex

	// Hosts to release or scrub at specified time, used by manageHosts process
	toManageAt map[string]time.Time
}

// Function runs as routine and makes sure identified hosts pool fits the configuration
func (d *Driver) newDedicatedPoolWorker(name string, record DedicatedPoolRecord) *dedicatedPoolWorker {
	worker := &dedicatedPoolWorker{
		name:   name,
		driver: d,
		record: record,

		activeHosts:           make(map[string]ec2types.Host),
		pendingAvailableHosts: make(map[string]time.Time),
		toManageAt:            make(map[string]time.Time),
	}

	// Receiving amount of instances per dedicated host
	worker.fetchInstancesPerHost()

	go worker.backgroundProcess()

	log.Debugf("AWS: Created dedicated pool: %q", worker.name)

	return worker
}

func (w *dedicatedPoolWorker) AvailableCapacity(instanceType string) int64 {
	// Check if instance type fits the pool type
	if instanceType != w.record.Type {
		log.Warnf("AWS: dedicated %q: Incorrect pool type requested: %s", w.name, instanceType)
		return -1
	}

	var instCount int64

	if err := w.updateDedicatedHosts(); err != nil {
		w.activeHostsMu.RLock()
		log.Warnf("AWS: dedicated %q: Unable to update dedicated hosts list, continue with %q: %v", w.activeHostsUpdated, err)
		w.activeHostsMu.RUnlock()
	}

	// Looking for the available hosts in the list and their capacity
	w.activeHostsMu.RLock()
	defer w.activeHostsMu.RUnlock()
	for _, host := range w.activeHosts {
		// For now support only single-type dedicated hosts, because primary goal is mac machines
		instCount += int64(getHostCapacity(&host))
	}

	// Let's add the amount of instances we can allocate
	instCount += (int64(w.record.Max) - int64(len(w.activeHosts))) * int64(w.instancesPerHost)

	log.Debugf("AWS: dedicated %q: AvailableCapacity for dedicated host type %q: %d", w.name, w.record.Type, instCount)

	return instCount
}

// Internally reserves the existing dedicated host if possible till the next list update
func (w *dedicatedPoolWorker) ReserveHost(instanceType string) (string, string) {
	if instanceType != w.record.Type {
		log.Warnf("AWS: dedicated %q: Incorrect pool type requested: %s", w.name, instanceType)
		return "", ""
	}

	// Using write lock here because it modifies the list of hosts in the end
	w.activeHostsMu.Lock()
	defer w.activeHostsMu.Unlock()

	var availableHosts []string

	// Look for the hosts with capacity
	for hostID, host := range w.activeHosts {
		if getHostCapacity(&host) > 0 {
			availableHosts = append(availableHosts, hostID)
		}
	}

	if len(availableHosts) < 1 {
		log.Infof("AWS: dedicated %q: No available hosts found in the current active list", w.name)
		return "", ""
	}

	// Pick random one from the list of available hosts to reduce the possibility of conflict
	host := w.activeHosts[availableHosts[rand.Intn(len(availableHosts))]] // #nosec G404
	// Mark it as reserved temporary to ease multi-allocation at the same time
	host.State = HostReserved
	w.activeHosts[aws.ToString(host.HostId)] = host
	return aws.ToString(host.HostId), aws.ToString(host.AvailabilityZone)
}

// Allocates the new dedicated host if possible
func (w *dedicatedPoolWorker) AllocateHost(instanceType string) (string, string) {
	if instanceType != w.record.Type {
		log.Warnf("AWS: dedicated %q: Incorrect pool type requested: %s", w.name, instanceType)
		return "", ""
	}

	currActiveHosts := len(w.activeHosts)
	if w.record.Max <= uint(currActiveHosts) {
		log.Warnf("AWS: dedicated %q: Unable to request new host due to reached the maximum limit: %d <= %d", w.name, w.record.Max, currActiveHosts)
		return "", ""
	}

	host, zone, err := w.allocateDedicatedHost()
	if err != nil || host == "" {
		log.Errorf("AWS: dedicated %q: Failed to allocate the new host: %v", w.name, err)
		return "", ""
	}

	return host, zone
}

// Will reserve existing or allocate the new host
func (w *dedicatedPoolWorker) ReserveAllocateHost(instanceType string) (string, string) {
	if instanceType != w.record.Type {
		log.Warnf("AWS: dedicated %q: Incorrect pool type requested: %s", w.name, instanceType)
		return "", ""
	}

	host, zone := w.ReserveHost(instanceType)
	if host != "" {
		return host, zone
	}
	return w.AllocateHost(instanceType)
}

func (w *dedicatedPoolWorker) fetchInstancesPerHost() {
	if strings.HasSuffix(w.record.Type, ".metal") {
		// We don't need to continue because metal == metal and means 1:1 capacity
		w.instancesPerHost = 1
		return
	}

	// Getting types to find dedicated host capacity
	// Adding the same type but with .metal on the end
	dotPos := strings.Index(w.record.Type, ".")
	if dotPos == -1 {
		dotPos = len(w.record.Type)
	}
	hostType := w.record.Type[0:dotPos] + ".metal"
	types := []string{w.record.Type, hostType}

	// We will not end until this works as expected. Not great in case user messed up with config,
	// but at least it will not leave the AWS driver not operational.
	conn := w.driver.newEC2Conn()
	for {
		instTypes, err := w.driver.getTypes(conn, types)
		if err != nil {
			log.Errorf("AWS: dedicated %q: Unable to get types %q (will retry): %v", w.name, types, err)
			time.Sleep(10 * time.Second)
			continue
		}

		instVcpus := aws.ToInt32(instTypes[w.record.Type].VCpuInfo.DefaultVCpus)
		hostVcpus := aws.ToInt32(instTypes[hostType].VCpuInfo.DefaultVCpus)
		w.instancesPerHost = uint(hostVcpus / instVcpus)
		log.Debugf("AWS: dedicated %q: Fetched amount of instances per host: %d", w.name, w.instancesPerHost)
		return
	}
}

// Runs function which holds the dedicated pool worker and executes it's processes
func (w *dedicatedPoolWorker) backgroundProcess() {
	defer log.Infof("AWS: dedicated %q: Exited backgroundProcess()", w.name)

	// Updating hosts and start background process for periodic update
	w.updateDedicatedHosts()
	go w.updateDedicatedHostsProcess()

	// Run main management process until fish stops
	for {
		// Running the manageHosts process
		w.releaseHosts(w.manageHosts())
		time.Sleep(10 * time.Second)
	}
}

// Runs periodically to keep the hosts pool busy and cheap
// Will return the list of hosts to release or exetute a scrubbing process for macs
func (w *dedicatedPoolWorker) manageHosts() []string {
	w.activeHostsMu.RLock()
	defer w.activeHostsMu.RUnlock()

	// List of hosts to clean from w.to_manage_at list
	var toClean []string
	var toRelease []string

	// Going through the process list
	for hostID, timeout := range w.toManageAt {
		if host, ok := w.activeHosts[hostID]; !ok || isHostUsed(&host) {
			// The host is disappeared or used, we don't need to manage it out anymore
			toClean = append(toClean, hostID)
			continue
		}

		// Host seems still exists and not used - check for timeout
		if timeout.Before(time.Now()) {
			// Timeout for the host reached - let's put it in the release bucket
			toRelease = append(toRelease, hostID)
		}
	}

	// Cleaning up the manage list
	for _, hostID := range toClean {
		delete(w.toManageAt, hostID)
	}

	// Going through the active hosts and updating to_manage list
	for hostID, host := range w.activeHosts {
		if host.State == ec2types.AllocationStatePermanentFailure {
			// Immediately release - we don't need failed hosts in our pool
			toRelease = append(toRelease, hostID)
		}

		// We don't need to manage out the hosts in use
		if isHostUsed(&host) {
			continue
		}

		// If mac host not too old and in scrubbing process (pending) - we don't need to bother
		if host.State == ec2types.AllocationStatePending && isHostMac(&host) && !w.isHostTooOld(&host) {
			continue
		}

		// Skipping the hosts that already in managed list
		found := false
		for hid := range w.toManageAt {
			if hostID == hid {
				found = true
				break
			}
		}
		if found {
			continue
		}

		// Check if mac - giving it some time before action release or scrubbing
		// If not mac or mac is old: giving a chance to be reused - will be processed next cycle
		if isHostMac(&host) && !w.isHostTooOld(&host) {
			w.toManageAt[hostID] = time.Now().Add(time.Duration(w.record.ScrubbingDelay))
		} else {
			w.toManageAt[hostID] = time.Now()
		}
		log.Debugf("AWS: dedicated %q: Added new host to be managed out: %q at %q", w.name, hostID, w.toManageAt[hostID])
	}

	return toRelease
}

func (w *dedicatedPoolWorker) releaseHosts(releaseHosts []string) {
	if len(releaseHosts) < 1 {
		// Skipping since there is nothing to do
		return
	}

	log.Debugf("AWS: dedicated %q: Dealing with hosts to release: %v", w.name, releaseHosts)

	// Function removes the items from the active hosts map to optimize the processes
	w.activeHostsMu.Lock()
	defer w.activeHostsMu.Unlock()

	// Check if there are macs which need a special treatment
	var macHosts []string
	var toRelease []string
	for _, hostID := range releaseHosts {
		// Special filtering for mac hosts and check if host is ready to be released. It's needed
		// to obey the rules of mac minimum life for 24h due to Apple-AWS license and in case you
		// need to keep the allocated dedicated hosts for longer then minimum needed release time.
		if host, ok := w.activeHosts[hostID]; ok && host.HostProperties != nil {
			if isHostMac(&host) {
				macHosts = append(macHosts, hostID)
			}
			// If the host not reached ReleaseDelay since allocation - skipping addition to list
			if !w.isHostReadyForRelease(&host) {
				continue
			}
		}
		// Adding any host to to_release list
		toRelease = append(toRelease, hostID)
	}

	// Run the release process for multiple hosts
	releaseFailed, err := w.releaseDedicatedHosts(toRelease)
	if err != nil {
		log.Errorf("AWS: dedicated %q: Unable to send request for release of the hosts %v: %v", w.name, toRelease, err)
		// Not fatal, because we still need to deal with mac hosts
	}

	// Cleanup the released hosts from the active hosts list
	for _, hostID := range toRelease {
		// Skipping if release of the host failed for some reason
		for _, failedHostID := range releaseFailed {
			if failedHostID == hostID {
				continue
			}
		}

		delete(w.activeHosts, hostID)
	}

	// Scrubbing the rest of mac hosts
	if len(macHosts) > 0 && w.record.ScrubbingDelay != 0 {
		for _, hostID := range macHosts {
			host, ok := w.activeHosts[hostID]
			if !ok || host.State == ec2types.AllocationStatePending {
				// The host was released or already in scrubbing - skipping it
				continue
			}

			// Reserve the host internally for scrubbing process to prevent allocation issues
			host.State = HostReserved
			w.activeHosts[aws.ToString(host.HostId)] = host

			// Triggering the scrubbing process
			if err := w.driver.triggerHostScrubbing(hostID, aws.ToString(host.HostProperties.InstanceType)); err != nil {
				log.Errorf("AWS: dedicated %q: Unable to run scrubbing for host %q: %v", w.name, hostID, err)
				continue
			}

			// Removing the host from the list
			delete(w.activeHosts, hostID)
		}
	}
}

func isHostMac(host *ec2types.Host) bool {
	return host.HostProperties != nil && awsInstTypeAny(aws.ToString(host.HostProperties.InstanceType), "mac")
}

func (w *dedicatedPoolWorker) isHostTooOld(host *ec2types.Host) bool {
	return aws.ToTime(host.AllocationTime).Before(time.Now().Add(-time.Duration(w.record.ReleaseDelay)))
}

// Check if the host is ready to be released - if it's mac then it should be older then 24h
func (w *dedicatedPoolWorker) isHostReadyForRelease(host *ec2types.Host) bool {
	// Host not used - for sure ready for release
	if !isHostUsed(host) {
		// If mac is not old enough - it's not ready for release
		if !w.isHostTooOld(host) {
			return false
		}
		return true
	}

	// Host in scrubbing process (pending) can be released but should be old enough
	if host.State == ec2types.AllocationStatePending && w.isHostTooOld(host) {
		return true
	}

	return false
}

// Check if the host is used
func isHostUsed(host *ec2types.Host) bool {
	if host.State == HostReserved || len(host.Instances) > 0 {
		return true
	}
	return false
}

// Check how much capacity we have on a host
func getHostCapacity(host *ec2types.Host) uint {
	if host.State != ec2types.AllocationStateAvailable || host.AvailableCapacity == nil {
		return 0
	}
	// TODO: For now supports only single-type dedicated hosts
	return uint(aws.ToInt32(host.AvailableCapacity.AvailableInstanceCapacity[0].AvailableCapacity))
}

// Updates the hosts list every 5 minutes
func (w *dedicatedPoolWorker) updateDedicatedHostsProcess() ([]ec2types.Host, error) {
	defer log.Infof("AWS: dedicated %q: Exited updateDedicatedHostsProcess()", w.name)

	// Balancing the regular update delay based on the scrubbing optimization because it needs to
	// record the time of host state change and only then the timer to scrubbing will start ticking
	updateDelay := 5 * time.Minute // 5 min by default
	scrubbingDelay := time.Duration(w.record.ScrubbingDelay)
	if scrubbingDelay != 0 && scrubbingDelay < 10*time.Minute {
		updateDelay = scrubbingDelay / 2
	}

	for {
		// Running check every 10 seconds
		time.Sleep(10 * time.Second)

		// Going through the list of newly available hosts to apply if PendingToAvailableDelay is set
		if w.record.PendingToAvailableDelay > 0 {
			w.pendingAvailableHostsMu.Lock()
			for hostID, t := range w.pendingAvailableHosts {
				if t.Before(time.Now()) {
					w.activeHostsMu.Lock()
					delete(w.pendingAvailableHosts, hostID)
					if host, ok := w.activeHosts[hostID]; ok {
						log.Debugf("AWS: dedicated %q: Making host %s available after pending", w.name, hostID)
						host.State = ec2types.AllocationStateAvailable
						w.activeHosts[hostID] = host
					}
					w.activeHostsMu.Unlock()
				}
			}
			w.pendingAvailableHostsMu.Unlock()
		}

		// We need to keep the request rate budget, so using a delay between regular updates.
		// If the dedicated hosts are used often, it could wait for a while due to often updates
		w.activeHostsMu.RLock()
		lastUpdate := w.activeHostsUpdated
		w.activeHostsMu.RUnlock()
		if lastUpdate.Before(time.Now().Add(-updateDelay)) {
			if err := w.updateDedicatedHosts(); err != nil {
				log.Warnf("AWS: dedicated %q: Error happened during the regular hosts update, continue with updated on %q: %v", w.name, lastUpdate, err)
			}
		}
	}
}

// Will list all the allocated dedicated hosts on AWS with desired zone and tag
func (w *dedicatedPoolWorker) updateDedicatedHosts() error {
	w.activeHostsMu.Lock()
	defer w.activeHostsMu.Unlock()

	// We should not update the list too often
	readyForUpdate := w.activeHostsUpdated.Before(time.Now().Add(-30 * time.Second))
	if !readyForUpdate {
		return nil
	}

	log.Debugf("AWS: dedicated %q: Updating dedicated pool hosts list", w.name)
	conn := w.driver.newEC2Conn()

	input := ec2.DescribeHostsInput{
		Filter: []ec2types.Filter{
			// We don't need released hosts, so skipping them
			{
				Name: aws.String("state"),
				Values: []string{
					string(ec2types.AllocationStateAvailable),
					string(ec2types.AllocationStateUnderAssessment),
					string(ec2types.AllocationStatePermanentFailure),
					string(ec2types.AllocationStatePending),
				},
			},
			{
				Name:   aws.String("availability-zone"),
				Values: w.record.Zones,
			},
			{
				Name:   aws.String("instance-type"),
				Values: []string{w.record.Type},
			},
			{
				Name:   aws.String("tag-key"),
				Values: []string{"AquariumDedicatedPool-" + w.name},
			},
		},
	}
	p := ec2.NewDescribeHostsPaginator(conn, &input)

	// Processing the hosts
	currActiveHosts := make(map[string]ec2types.Host)
	for p.HasMorePages() {
		resp, err := p.NextPage(context.TODO())
		if err != nil {
			return log.Errorf("AWS: dedicated %q: Error during requesting dedicated hosts: %v", w.name, err)
		}

		for _, rh := range resp.Hosts {
			hostID := aws.ToString(rh.HostId)
			currActiveHosts[hostID] = rh
			// Check if we have this host in the list already
			if ah, ok := w.activeHosts[hostID]; ok {
				// When PendingToAvailableDelay is set we use special process to switch from pending state to Available
				if w.record.PendingToAvailableDelay > 0 {
					if ah.State == ec2types.AllocationStatePending && rh.State == ec2types.AllocationStateAvailable {
						w.pendingAvailableHostsMu.Lock()
						if _, ok := w.pendingAvailableHosts[hostID]; !ok {
							delayTill := time.Now().Add(time.Duration(w.record.PendingToAvailableDelay))
							log.Debugf("AWS: dedicated %q: Delaying availability of host %s till %s", w.name, hostID, delayTill)
							w.pendingAvailableHosts[hostID] = delayTill
						}
						w.pendingAvailableHostsMu.Unlock()
						// Updating the status each run to make sure it will not switch to Available before delay is out
						host := currActiveHosts[hostID]
						host.State = ec2types.AllocationStatePending
						currActiveHosts[hostID] = host
					} else if rh.State != ec2types.AllocationStateAvailable {
						// If the state changed from Available - removing the item
						w.pendingAvailableHostsMu.Lock()
						if _, ok := w.pendingAvailableHosts[hostID]; ok {
							log.Debugf("AWS: dedicated %q: Host state changed, so removing host %s from pendingAvailableHosts", w.name, hostID)
							delete(w.pendingAvailableHosts, hostID)
						}
						w.pendingAvailableHostsMu.Unlock()
					}
				}
			}
		}
	}

	// Updating the list of hosts with received data
	w.activeHostsUpdated = time.Now()
	w.activeHosts = currActiveHosts

	// Printing list for debug purposes
	if log.GetVerbosity() == 1 {
		log.Debugf("AWS: dedicated %q: Amount of active hosts in pool: %d", w.name, len(w.activeHosts))
		for hostID, host := range w.activeHosts {
			log.Debugf("AWS: dedicated %q: active_hosts item: host_id:%q, allocated:%q, state:%q, capacity:%d (%d)", w.name, hostID, host.AllocationTime, host.State, getHostCapacity(&host), w.instancesPerHost)
		}
	}

	return nil
}

func (w *dedicatedPoolWorker) allocateDedicatedHost() (string, string, error) {
	log.Infof("AWS: dedicated %q: Allocating dedicated host of type %q", w.name, w.record.Type)

	// Storing happened issues to later show in log as error
	errors := []string{}
	conn := w.driver.newEC2Conn()

	for _, zone := range w.record.Zones {
		input := ec2.AllocateHostsInput{
			AvailabilityZone: aws.String(zone),
			AutoPlacement:    ec2types.AutoPlacementOff, // Managed hosts are for targeted workload
			InstanceType:     aws.String(w.record.Type),
			Quantity:         aws.Int32(1),

			TagSpecifications: []ec2types.TagSpecification{{
				ResourceType: ec2types.ResourceTypeDedicatedHost,
				Tags: []ec2types.Tag{
					{
						Key:   aws.String("AquariumDedicatedPoolName"),
						Value: aws.String(w.name),
					},
					// Needed to simplify the filtering for list, because Input filter doesn't support tag:<KEY>
					{
						Key:   aws.String("AquariumDedicatedPool-" + w.name),
						Value: aws.String(""),
					},
				},
			}},
		}

		// SDK can't return the partially executed request (where some of the hosts are allocated)
		resp, err := conn.AllocateHosts(context.TODO(), &input)
		if err != nil {
			if !slices.Contains(errors, err.Error()) {
				errors = append(errors, err.Error())
			}
			log.Debugf("AWS: dedicated %q: Unable to allocate dedicated hosts in zone %s: %v", w.name, zone, err)
			continue
		}

		log.Infof("AWS: dedicated %q: Allocated host in zone %s: %v", w.name, zone, resp.HostIds[0])

		return resp.HostIds[0], zone, nil
	}

	return "", "", log.Errorf("AWS: dedicated %q: Unable to allocate dedicated hosts in zones %s: %v", w.name, w.record.Zones, errors)
}

// Will request a release for a bunch of hosts and return unsuccessful id's or error
func (w *dedicatedPoolWorker) releaseDedicatedHosts(ids []string) ([]string, error) {
	if len(ids) < 1 {
		return ids, nil
	}
	log.Infof("AWS: dedicated %q: Releasing %d dedicated hosts: %v", w.name, len(ids), ids)

	conn := w.driver.newEC2Conn()

	input := ec2.ReleaseHostsInput{HostIds: ids}

	resp, err := conn.ReleaseHosts(context.TODO(), &input)
	if err != nil {
		return ids, log.Errorf("AWS: dedicated %q: Unable to release dedicated hosts: %v", w.name, err)
	}

	var unsuccessful []string
	if len(resp.Unsuccessful) > 0 {
		failedInfo := ""
		for _, item := range resp.Unsuccessful {
			failedInfo += fmt.Sprintf("- InstanceId: %s\n  Error: %s %q\n", aws.ToString(item.ResourceId), aws.ToString(item.Error.Code), aws.ToString(item.Error.Message))
			unsuccessful = append(unsuccessful, aws.ToString(item.ResourceId))
		}

		log.Warnf("AWS: dedicated %q: Not all the hosts were released as requested:\n%v", w.name, failedInfo)
	}
	log.Infof("AWS: dedicated %q: Released hosts: %v", w.name, resp.Successful)

	return unsuccessful, nil
}
