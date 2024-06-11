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

package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/util"
)

type Config struct {
	Region    string `json:"region"`     // AWS Region to connect to
	KeyID     string `json:"key_id"`     // AWS AMI Key ID
	SecretKey string `json:"secret_key"` // AWS AMI Secret Key

	// Optional
	AccountIDs   []string          `json:"account_ids"`   // AWS Trusted account IDs to filter vpc, subnet, sg, images, snapshots...
	InstanceTags map[string]string `json:"instance_tags"` // AWS Instance tags to use when this node provision them

	// Manage the AWS dedicated hosts to keep them busy and deallocate when not needed
	// Key of the map is name of the pool - will be used for identification of the pool
	DedicatedPool map[string]DedicatedPoolRecord `json:"dedicated_pool"`
}

// Stores the configuration of AWS dedicated pool of particular type to manage
// aws ec2 allocate-hosts --availability-zone "us-west-2c" --auto-placement "on" --host-recovery "off" --host-maintenance "off" --quantity 1 --instance-type "mac2.metal"
type DedicatedPoolRecord struct {
	Type string `json:"type"` // Instance type handled by the dedicated hosts pool (example: "mac2.metal")
	Zone string `json:"zone"` // Where to allocate the dedicated host (example: "us-west-2c")
	Max  uint   `json:"max"`  // Maximum dedicated hosts to allocate (they sometimes can handle more than 1 capacity slot)

	// Optimization for the Mac dedicated hosts to send them in [scrubbing process] to save money
	// (scrubbing is free but takes ~1-2h) when we can't release the host ([24h min limit]). When
	// this option is set to 0 - no scrubbing is enabled. When it's set - then it's duration to
	// stay idle and then run and terminate empty instance to trigger scrubbing. Without this delay
	// the host will have no time to be utilized by the new workload, which is not good from
	// utilization perspective.
	//
	// Current implementation is attached to state update, which could be API consuming, so this
	// duration should be >= 1 min, otherwise API requests will be too often.
	//
	// [scrubbing process]: https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/mac-instance-stop.html
	// [24h min limit]: https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ec2-mac-instances.html#mac-instance-considerations
	ScrubbingDelay util.Duration `json:"scrubbing_delay"`
}

func (c *Config) Apply(config []byte) error {
	// Parse json
	if len(config) > 0 {
		if err := json.Unmarshal(config, c); err != nil {
			return log.Error("AWS: Unable to apply the driver config:", err)
		}
	}

	return nil
}

func (c *Config) Validate() (err error) {
	// Check that values of the config is filled at least with defaults
	if c.Region == "" {
		return fmt.Errorf("AWS: No EC2 region is specified")
	}

	if c.KeyID == "" {
		return fmt.Errorf("AWS: Credentials Key ID is not set")
	}
	if c.SecretKey == "" {
		return fmt.Errorf("AWS: Credentials SecretKey is not set")
	}

	// Verify that connection is possible with those creds and get the account ID
	conn := sts.NewFromConfig(aws.Config{
		Region: c.Region,
		Credentials: aws.CredentialsProviderFunc(func(ctx context.Context) (aws.Credentials, error) {
			return aws.Credentials{
				AccessKeyID:     c.KeyID,
				SecretAccessKey: c.SecretKey,
				Source:          "fish-cfg",
			}, nil
		}),
	})
	input := &sts.GetCallerIdentityInput{}

	// Checking the connection for 1 minute in case network is unavailable
	// It helps with the machines where internet is not available right away
	retries := 6
	counter := 0
	account := ""
	for {
		res, err := conn.GetCallerIdentity(context.TODO(), input)
		counter++
		if err != nil {
			if counter < retries {
				log.Warn("AWS: Retry after credentials validation error:", err)
				// Give command 10 seconds to rest
				time.Sleep(10 * time.Second)
				continue
			}
		}
		if err != nil {
			return fmt.Errorf("AWS: Unable to verify connection by calling STS service: %v", err)
		}
		account = *res.Account
		break
	}
	if len(c.AccountIDs) > 0 && c.AccountIDs[0] != account {
		log.Debug("AWS: Using Account IDs:", c.AccountIDs, "(real: ", account, ")")
	} else {
		c.AccountIDs = []string{account}
		log.Debug("AWS: Using Account IDs:", c.AccountIDs)
	}

	// Init empty instance tags in case its not set
	if c.DedicatedPool == nil {
		c.InstanceTags = make(map[string]string)
	}
	// Init empty dedicated pool in case its not set
	if c.DedicatedPool == nil {
		c.DedicatedPool = make(map[string]DedicatedPoolRecord)
	}
	// Make sure the ScrubbingDelay either unset or >= 1min or we will face often update API reqs
	for name, pool := range c.DedicatedPool {
		if pool.ScrubbingDelay > 0 && time.Duration(pool.ScrubbingDelay) < 1*time.Minute {
			return fmt.Errorf("AWS: Scrubbing delay of pool %q is less then 1 minute: %v", name, pool.ScrubbingDelay)
		}
	}

	return nil
}
