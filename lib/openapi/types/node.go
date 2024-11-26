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

package types

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"

	"github.com/adobe/aquarium-fish/lib/log"
)

// NodePingDelay defines delay between the pings to keep the node active in the cluster
const NodePingDelay = 10

// TODO: Need to restore this functionality to not allow node duplicates join the cluster
var NodePingDuplicationErr = fmt.Errorf("Fish Node: Unable to join the Aquarium cluster due to " +
	"the node with the same name pinged the cluster less then 2xNODE_PING_DELAY time ago")

// Init prepares Node for usage
func (n *Node) Init(nodeAddress, certPath string) error {
	// Set the node external address
	n.Address = nodeAddress

	// Read certificate's pubkey to put or compare
	certBytes, err := os.ReadFile(certPath)
	if err != nil {
		return err
	}
	block, _ := pem.Decode(certBytes)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return err
	}
	pubkeyDer, err := x509.MarshalPKIXPublicKey(cert.PublicKey)
	if err != nil {
		return err
	}

	// Validate the existing node pubkey
	if n.Pubkey != nil && !bytes.Equal(*n.Pubkey, pubkeyDer) {
		log.Warn("Fish Node: The pubkey was changed for the Node, replacing it with the new one")
	}

	n.Pubkey = &pubkeyDer

	return nil
}
