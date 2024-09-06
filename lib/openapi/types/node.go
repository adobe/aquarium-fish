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
)

const NodePingDelay = 10

var ErrNodePingDuplication = fmt.Errorf("Fish Node: Unable to join the Aquarium cluster due to " +
	"the node with the same name pinged the cluster less then 2xNODE_PING_DELAY time ago")

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

	// Update the pubkey once - it can not be changed for the node name for now,
	// maybe later the process of key switch will be implemented
	if n.Pubkey == nil {
		// Set the pubkey once
		n.Pubkey = &pubkeyDer
	} else {
		// Validate the existing pubkey
		if !bytes.Equal(*n.Pubkey, pubkeyDer) {
			return fmt.Errorf("Fish Node: The pubkey was changed for Node, that's not supported")
		}
	}

	// Collect the node definition data
	n.Definition.Update()

	return nil
}
