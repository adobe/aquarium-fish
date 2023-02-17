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

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/adobe/aquarium-fish/lib/log"
)

type Config struct {
	Region    string `json:"region"`     // AWS Region to connect to
	KeyID     string `json:"key_id"`     // AWS AMI Key ID
	SecretKey string `json:"secret_key"` // AWS AMI Secret Key

	// Optional
	AccountIDs   []string          `json:"account_ids"`   // AWS Trusted account IDs to filter vpc, subnet, sg, images, snapshots...
	InstanceTags map[string]string `json:"instance_tags"` // AWS Instance tags to use when this node provision them
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
	res, err := conn.GetCallerIdentity(context.TODO(), input)
	if err != nil {
		return fmt.Errorf("AWS: Unable to verify connection by calling STS service: %v", err)
	}
	if len(c.AccountIDs) > 0 && c.AccountIDs[0] != *res.Account {
		log.Debug("AWS: Using Account IDs:", c.AccountIDs, "(real: ", *res.Account, ")")
	} else {
		c.AccountIDs = []string{*res.Account}
		log.Debug("AWS: Using Account IDs:", c.AccountIDs)
	}

	return nil
}
