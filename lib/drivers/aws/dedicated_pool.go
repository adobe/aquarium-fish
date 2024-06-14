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
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2_types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/adobe/aquarium-fish/lib/log"
)

// Custom status to set in the host for simplifying parallel ops in between the updates
const HOST_RESERVED = "reserved"

// TODO: Right now logic pinned to just one node, need to be distributed

// This structure keeps the available list of hosts & state to operate on hosts management
type dedicatedPoolWorker struct {
	name   string
	driver *Driver
	record DedicatedPoolRecord

	// Amount of instances per dedicated host used in capacity calculations
	instances_per_host uint

	// It's better to update active_hosts by calling updateDedicatedHosts()
	active_hosts         map[string]ec2_types.Host
	active_hosts_mu      sync.RWMutex
	active_hosts_updated time.Time

	// Hosts to release or scrub at specified time, used by manageHosts process
	to_manage_at map[string]time.Time
}

// Function runs as routine and makes sure identified hosts pool fits the configuration
func (d *Driver) newDedicatedPoolWorker(name string, record DedicatedPoolRecord) *dedicatedPoolWorker {
	worker := &dedicatedPoolWorker{
		name:   name,
		driver: d,
		record: record,

		active_hosts: make(map[string]ec2_types.Host),
		to_manage_at: make(map[string]time.Time),
	}

	// Receiving amount of instances per dedicated host
	worker.fetchInstancesPerHost()

	go worker.backgroundProcess()

	log.Debugf("AWS: Created dedicated pool: %q", worker.name)

	return worker
}

func (w *dedicatedPoolWorker) AvailableCapacity(instance_type string) int64 {
	// Check if instance type fits the pool type
	if instance_type != w.record.Type {
		log.Warnf("AWS: dedicated %q: Incorrect pool type requested: %s", w.name, instance_type)
		return -1
	}

	var inst_count int64

	if err := w.updateDedicatedHosts(); err != nil {
		log.Warnf("AWS: dedicated %q: Unable to update dedicated hosts list, continue with %q: %v", w.active_hosts_updated, err)
	}

	// Looking for the available hosts in the list and their capacity
	w.active_hosts_mu.RLock()
	defer w.active_hosts_mu.RUnlock()
	for _, host := range w.active_hosts {
		// For now support only single-type dedicated hosts, because primary goal is mac machines
		inst_count += int64(getHostCapacity(&host))
	}

	// Let's add the amount of instances we can allocate
	inst_count += (int64(w.record.Max) - int64(len(w.active_hosts))) * int64(w.instances_per_host)

	log.Debugf("AWS: dedicated %q: AvailableCapacity for dedicated host type %q: %d", w.name, w.record.Type, inst_count)

	return inst_count
}

// Internally reserves the existing dedicated host if possible till the next list update
func (w *dedicatedPoolWorker) ReserveHost(instance_type string) string {
	if instance_type != w.record.Type {
		log.Warnf("AWS: dedicated %q: Incorrect pool type requested: %s", w.name, instance_type)
		return ""
	}

	// Using write lock here because it modifies the list of hosts in the end
	w.active_hosts_mu.Lock()
	defer w.active_hosts_mu.Unlock()

	var available_hosts []string

	// Look for the hosts with capacity
	for host_id, host := range w.active_hosts {
		if getHostCapacity(&host) > 0 {
			available_hosts = append(available_hosts, host_id)
		}
	}

	if len(available_hosts) < 1 {
		log.Infof("AWS: dedicated %q: No available hosts found in the current active list", w.name)
		return ""
	}

	// Pick random one from the list of available hosts to reduce the possibility of conflict
	host := w.active_hosts[available_hosts[rand.Intn(len(available_hosts))]]
	// Mark it as reserved temporarly to ease multi-allocation at the same time
	host.State = HOST_RESERVED
	w.active_hosts[aws.ToString(host.HostId)] = host
	return aws.ToString(host.HostId)
}

// Allocates the new dedicated host if possible
func (w *dedicatedPoolWorker) AllocateHost(instance_type string) string {
	if instance_type != w.record.Type {
		log.Warnf("AWS: dedicated %q: Incorrect pool type requested: %s", w.name, instance_type)
		return ""
	}

	curr_active_hosts := len(w.active_hosts)
	if w.record.Max <= uint(curr_active_hosts) {
		log.Warnf("AWS: dedicated %q: Unable to request new host due to reached the maximum limit: %d <= %d", w.name, w.record.Max, curr_active_hosts)
		return ""
	}

	hosts, err := w.allocateDedicatedHosts(1)
	if err != nil || len(hosts) < 1 {
		log.Errorf("AWS: dedicated %q: Failed to allocate the new host: %v", w.name, err)
		return ""
	}

	return hosts[0]
}

// Will reserve existing or allocate the new host
func (w *dedicatedPoolWorker) ReserveAllocateHost(instance_type string) string {
	if instance_type != w.record.Type {
		log.Warnf("AWS: dedicated %q: Incorrect pool type requested: %s", w.name, instance_type)
		return ""
	}

	out := w.ReserveHost(instance_type)
	if out != "" {
		return out
	}
	return w.AllocateHost(instance_type)
}

func (w *dedicatedPoolWorker) fetchInstancesPerHost() {
	if strings.HasSuffix(w.record.Type, ".metal") {
		// We don't need to continue because metal == metal and means 1:1 capacity
		w.instances_per_host = 1
		return
	}

	// Getting types to find dedicated host capacity
	// Adding the same type but with .metal on the end
	dot_pos := strings.Index(w.record.Type, ".")
	if dot_pos == -1 {
		dot_pos = len(w.record.Type)
	}
	host_type := w.record.Type[0:dot_pos] + ".metal"
	types := []string{w.record.Type, host_type}

	// We will not end until this works as expected. Not great in case user messed up with config,
	// but at least it will not leave the AWS driver not operational.
	conn := w.driver.newEC2Conn()
	for {
		inst_types, err := w.driver.getTypes(conn, types)
		if err != nil {
			log.Errorf("AWS: dedicated %q: Unable to get types %q (will retry): %v", w.name, types, err)
			time.Sleep(10 * time.Second)
			continue
		}

		inst_vcpus := aws.ToInt32(inst_types[w.record.Type].VCpuInfo.DefaultVCpus)
		host_vcpus := aws.ToInt32(inst_types[host_type].VCpuInfo.DefaultVCpus)
		w.instances_per_host = uint(host_vcpus / inst_vcpus)
		log.Debugf("AWS: dedicated %q: Fetched amount of instances per host: %d", w.name, w.instances_per_host)
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
	w.active_hosts_mu.RLock()
	defer w.active_hosts_mu.RUnlock()

	// List of hosts to clean from w.to_manage_at list
	var to_clean []string
	var to_release []string

	// Going through the process list
	for host_id, timeout := range w.to_manage_at {
		if host, ok := w.active_hosts[host_id]; !ok || isHostUsed(&host) {
			// The host is disappeared or used, we don't need to manage it out anymore
			to_clean = append(to_clean, host_id)
			continue
		}

		// Host seems still exists and not used - check for timeout
		if timeout.Before(time.Now()) {
			// Timeout for the host reached - let's put it in the release bucket
			to_release = append(to_release, host_id)
		}
	}

	// Cleaning up the manage list
	for _, host_id := range to_clean {
		delete(w.to_manage_at, host_id)
	}

	// Going through the active hosts and updating to_manage list
	for host_id, host := range w.active_hosts {
		if host.State == ec2_types.AllocationStatePermanentFailure {
			// Immediately release - we don't need failed hosts in our pool
			to_release = append(to_release, host_id)
		}

		// We don't need to manage out the hosts in use
		if isHostUsed(&host) {
			continue
		}

		// If it's mac not too old and in scrubbing process (pending) - we don't need to bother
		if host.State == ec2_types.AllocationStatePending && isHostMac(&host) && !isMacTooOld(&host) {
			continue
		}

		// Skipping the hosts that already in managed list
		found := false
		for hid, _ := range w.to_manage_at {
			if host_id == hid {
				found = true
				break
			}
		}
		if found {
			continue
		}

		// Check if mac - giving it some time before action release or scrubbing
		// If not mac or mac is old: giving a chance to be reused - will be processed next cycle
		if isHostMac(&host) && !isMacTooOld(&host) {
			w.to_manage_at[host_id] = time.Now().Add(time.Duration(w.record.ScrubbingDelay))
		} else {
			w.to_manage_at[host_id] = time.Now()
		}
		log.Debugf("AWS: dedicated %q: Added new host to be managed out: %q at %q", w.name, host_id, w.to_manage_at[host_id])
	}

	return to_release
}

func (w *dedicatedPoolWorker) releaseHosts(release_hosts []string) {
	if len(release_hosts) < 1 {
		// Skipping since there is nothing to do
		return
	}

	log.Debugf("AWS: dedicated %q: Dealing with hosts to release: %v", w.name, release_hosts)

	// Function removes the items from the active hosts map to optimize the processes
	w.active_hosts_mu.Lock()
	defer w.active_hosts_mu.Unlock()

	// Check if there are macs which need a special treatment
	var mac_hosts []string
	var to_release []string
	for _, host_id := range release_hosts {
		// Special treatment for mac hosts - it makes not much sense to try to release them until
		// they've live for 24h due to Apple-AWS license.
		if host, ok := w.active_hosts[host_id]; ok && host.HostProperties != nil {
			if isHostMac(&host) {
				mac_hosts = append(mac_hosts, host_id)
				// If mac host not reached 24h since allocation - skipping addtion to the release list
				if !isHostReadyForRelease(&host) {
					continue
				}
			}
		}
		// Adding any host to to_release list
		to_release = append(to_release, host_id)
	}

	// Run the release process for multiple hosts
	release_failed, err := w.releaseDedicatedHosts(to_release)
	if err != nil {
		log.Errorf("AWS: dedicated %q: Unable to send request for release of the hosts %v: %v", w.name, to_release, err)
		// Not fatal, because we still need to deal with mac hosts
	}

	// Cleanup the released hosts from the active hosts list
	for _, host_id := range to_release {
		// Skipping if release of the host failed for some reason
		for _, failed_host_id := range release_failed {
			if failed_host_id == host_id {
				continue
			}
		}

		delete(w.active_hosts, host_id)
	}

	// Scrubbing the rest of mac hosts
	if len(mac_hosts) > 0 && w.record.ScrubbingDelay != 0 {
		for _, host_id := range mac_hosts {
			host, ok := w.active_hosts[host_id]
			if !ok || host.State == ec2_types.AllocationStatePending {
				// The host was released or already in scrubbing - skipping it
				continue
			}

			// Reserve the host internally for scrubbing process to prevent allocation issues
			host.State = HOST_RESERVED
			w.active_hosts[aws.ToString(host.HostId)] = host

			// Triggering the scrubbing process
			if err := w.driver.triggerHostScrubbing(host_id, aws.ToString(host.HostProperties.InstanceType)); err != nil {
				log.Errorf("AWS: dedicated %q: Unable to run scrubbing for host %q: %v", w.name, host_id, err)
				continue
			}

			// Removing the host from the list
			delete(w.active_hosts, host_id)
		}
	}
}

func isHostMac(host *ec2_types.Host) bool {
	return host.HostProperties != nil && awsInstTypeAny(aws.ToString(host.HostProperties.InstanceType), "mac")
}

func isMacTooOld(host *ec2_types.Host) bool {
	return aws.ToTime(host.AllocationTime).Before(time.Now().Add(-24 * time.Hour))
}

// Check if the host is ready to be released - if it's mac then it should be older then 24h
func isHostReadyForRelease(host *ec2_types.Host) bool {
	// Host not used - for sure ready for release (or scrubbing in case of young mac)
	if !isHostUsed(host) {
		return true
	}

	// Mac in scrubbing process (pending) can be released but should be older then 24h
	if host.State == ec2_types.AllocationStatePending && isHostMac(host) && isMacTooOld(host) {
		return true
	}

	return false
}

// Check if the host is used
func isHostUsed(host *ec2_types.Host) bool {
	if host.State == HOST_RESERVED || host.Instances != nil && len(host.Instances) > 0 {
		return true
	}
	return false
}

// Check how much capacity we have on a host
func getHostCapacity(host *ec2_types.Host) uint {
	if host.State != ec2_types.AllocationStateAvailable || host.AvailableCapacity == nil {
		return 0
	}
	// TODO: For now supports only single-type dedicated hosts
	return uint(aws.ToInt32(host.AvailableCapacity.AvailableInstanceCapacity[0].AvailableCapacity))
}

// Updates the hosts list every 5 minutes
func (w *dedicatedPoolWorker) updateDedicatedHostsProcess() ([]ec2_types.Host, error) {
	defer log.Infof("AWS: dedicated %q: Exited updateDedicatedHostsProcess()", w.name)

	// Balancing the regular update delay based on the scrubbing optimization because it needs to
	// record the time of host state change and only then the timer to scrubbing will start ticking
	update_delay := 5 * time.Minute // 5 min by default
	scrubbing_delay := time.Duration(w.record.ScrubbingDelay)
	if scrubbing_delay != 0 && scrubbing_delay < 10*time.Minute {
		update_delay = scrubbing_delay / 2
	}

	for {
		time.Sleep(30 * time.Second)
		// We need to keep the request rate budget, so using a delay between regular updates.
		// If the dedicated hosts are used often, it could wait for a while due to often updates
		if w.active_hosts_updated.Before(time.Now().Add(-update_delay)) {
			if err := w.updateDedicatedHosts(); err != nil {
				log.Warnf("AWS: dedicated %q: Error happened during the regular hosts update, continue with updated on %q: %v", w.active_hosts_updated, err)
			}
		}
	}
}

// Will list all the allocated dedicated hosts on AWS with desired zone and tag
func (w *dedicatedPoolWorker) updateDedicatedHosts() error {
	// Do not update too often
	if w.active_hosts_updated.After(time.Now().Add(-10 * time.Second)) {
		return nil
	}

	log.Debugf("AWS: dedicated %q: Updating dedicated pool hosts list", w.name)
	conn := w.driver.newEC2Conn()

	p := ec2.NewDescribeHostsPaginator(conn, &ec2.DescribeHostsInput{
		Filter: []ec2_types.Filter{
			// We don't need released hosts, so skipping them
			ec2_types.Filter{
				Name: aws.String("state"),
				Values: []string{
					string(ec2_types.AllocationStateAvailable),
					string(ec2_types.AllocationStateUnderAssessment),
					string(ec2_types.AllocationStatePermanentFailure),
					string(ec2_types.AllocationStatePending),
				},
			},
			ec2_types.Filter{
				Name:   aws.String("availability-zone"),
				Values: []string{w.record.Zone},
			},
			ec2_types.Filter{
				Name:   aws.String("instance-type"),
				Values: []string{w.record.Type},
			},
			ec2_types.Filter{
				Name:   aws.String("tag-key"),
				Values: []string{"AquariumDedicatedPool-" + w.name},
			},
		},
	})

	// Processing the hosts
	curr_active_hosts := make(map[string]ec2_types.Host)
	for p.HasMorePages() {
		resp, err := p.NextPage(context.TODO())
		if err != nil {
			return log.Errorf("AWS: dedicated %q: Error during requesting dedicated hosts: %v", w.name, err)
		}

		for _, rh := range resp.Hosts {
			host_id := aws.ToString(rh.HostId)
			curr_active_hosts[host_id] = rh
			// If the response host has not changed, use the same object in the active list
			if ah, ok := w.active_hosts[host_id]; ok && ah.State == rh.State && len(ah.Instances) == len(rh.Instances) {
				curr_active_hosts[host_id] = w.active_hosts[host_id]
			}
		}
	}

	// Updating the list of hosts with received data
	w.active_hosts_mu.Lock()
	defer w.active_hosts_mu.Unlock()

	w.active_hosts_updated = time.Now()
	w.active_hosts = curr_active_hosts

	// Printing list for debug purposes
	if log.Verbosity == 1 {
		log.Debugf("AWS: dedicated %q: Amount of active hosts in pool: %d", w.name, len(w.active_hosts))
		for host_id, host := range w.active_hosts {
			log.Debugf("AWS: dedicated %q: active_hosts item: host_id:%q, allocated:%q, state:%q, capacity:%d (%d)", w.name, host_id, host.AllocationTime, host.State, getHostCapacity(&host), w.instances_per_host)
		}
	}

	return nil
}

func (w *dedicatedPoolWorker) allocateDedicatedHosts(amount int32) ([]string, error) {
	log.Infof("AWS: dedicated %q: Allocating %d dedicated hosts of type %q", w.name, amount, w.record.Type)

	conn := w.driver.newEC2Conn()

	input := &ec2.AllocateHostsInput{
		AvailabilityZone: aws.String(w.record.Zone),
		AutoPlacement:    ec2_types.AutoPlacementOff, // Managed hosts are for targeted workload
		InstanceType:     aws.String(w.record.Type),
		Quantity:         aws.Int32(amount),

		TagSpecifications: []ec2_types.TagSpecification{{
			ResourceType: ec2_types.ResourceTypeDedicatedHost,
			Tags: []ec2_types.Tag{
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

	resp, err := conn.AllocateHosts(context.TODO(), input)
	if err != nil {
		return nil, log.Errorf("AWS: dedicated %q: Unable to allocate dedicated hosts: %v", w.name, err)
	}

	log.Infof("AWS: dedicated %q: Allocated hosts: %v", w.name, resp.HostIds)

	return resp.HostIds, nil
}

// Will request a release for a bunch of hosts and return unsuccessful id's or error
func (w *dedicatedPoolWorker) releaseDedicatedHosts(ids []string) ([]string, error) {
	if len(ids) < 1 {
		return ids, nil
	}
	log.Infof("AWS: dedicated %q: Releasing %d dedicated hosts: %v", w.name, len(ids), ids)

	conn := w.driver.newEC2Conn()

	input := &ec2.ReleaseHostsInput{HostIds: ids}

	resp, err := conn.ReleaseHosts(context.TODO(), input)
	if err != nil {
		return ids, log.Errorf("AWS: dedicated %q: Unable to release dedicated hosts: %v", w.name, err)
	}

	var unsuccessful []string
	if len(resp.Unsuccessful) > 0 {
		failed_info := ""
		for _, item := range resp.Unsuccessful {
			failed_info += fmt.Sprintf("- InstanceId: %s\n  Error: %v\n", aws.ToString(item.ResourceId), item.Error)
			unsuccessful = append(unsuccessful, aws.ToString(item.ResourceId))
		}

		log.Warnf("AWS: dedicated %q: Not all the hosts were released as requested:\n%v", w.name, failed_info)
	}
	log.Infof("AWS: dedicated %q: Released hosts: %v", w.name, resp.Successful)

	return unsuccessful, nil
}
