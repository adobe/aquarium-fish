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

package crypt

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// InitTLSPairCa creates a pair of asymmetric keys and CA if needed
func InitTLSPairCa(hosts []string, caPath, keyPath, crtPath string) error {
	// Generates simple CA and Node certificate signed by the CA
	_, caErr := os.Stat(caPath)
	if os.IsNotExist(caErr) {
		// Generate new CA since it's not exist
		if err := generateSimpleCa(getCaKeyFromCertPath(caPath), caPath); err != nil {
			return err
		}
	}

	_, keyErr := os.Stat(keyPath)
	_, crtErr := os.Stat(crtPath)
	if os.IsNotExist(keyErr) || os.IsNotExist(crtErr) {
		// Generate fish key & cert
		if err := generateSimpleKeyCert(hosts, keyPath, crtPath, caPath); err != nil {
			return err
		}
	}

	return nil
}

func getCaKeyFromCertPath(caPath string) string {
	// Just trim the name extension and add ".key"
	filename := filepath.Base(caPath)
	n := strings.LastIndexByte(filename, '.')
	if n == -1 {
		return caPath
	}
	return filepath.Join(filepath.Dir(caPath), filename[:n]+".key")
}

func generateSimpleCa(keyPath, crtPath string) error {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}

	notBefore := time.Now()

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return err
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			// It's just an example CA - for prod generate CA & certs yourself with openssl
			Organization: []string{"Example Co CA"},
			CommonName:   "ClusterCA",
		},

		NotBefore: notBefore,
		NotAfter:  notBefore.AddDate(10, 0, 0), // 10y

		IsCA:     true,
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,

		BasicConstraintsValid: true,
	}

	// Generate certificate
	if err := createCert(crtPath, &priv.PublicKey, priv, &template, &template); err != nil {
		return err
	}

	// Create private key file
	err = createKey(keyPath, priv)

	return err
}

func generateSimpleKeyCert(hosts []string, keyPath, crtPath, caPath string) error {
	// Load the CA key and cert
	caTLS, err := tls.LoadX509KeyPair(caPath, getCaKeyFromCertPath(caPath))
	if err != nil {
		return err
	}
	caKey := caTLS.PrivateKey

	caCrt, err := x509.ParseCertificate(caTLS.Certificate[0])
	if err != nil {
		return err
	}

	// Generate simple service sertificate
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}

	notBefore := time.Now()

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return err
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Example Co Crt"},
			CommonName:   hosts[0], // Node Name is first in hosts list
		},

		NotBefore: notBefore,
		NotAfter:  notBefore.AddDate(1, 0, 0), // 1y

		// Overall for server & client auth
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},

		BasicConstraintsValid: true,
	}

	for _, h := range hosts {
		// If the colon-part if port is specified too
		h = strings.SplitN(h, ":", 2)[0]
		if ip := net.ParseIP(h); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, h)
		}
	}

	// Generate certificate
	if err := createCert(crtPath, &priv.PublicKey, caKey, &template, caCrt); err != nil {
		return err
	}

	// Create private key file
	err = createKey(keyPath, priv)

	return err
}

func createCert(crtPath string, pubkey crypto.PublicKey, caKey crypto.PrivateKey, cert, caCrt *x509.Certificate) error {
	// Generate certificate
	derBytes, err := x509.CreateCertificate(rand.Reader, cert, caCrt, pubkey, caKey)
	if err != nil {
		return err
	}

	// Create certificate file
	crtOut, err := os.Create(crtPath)
	if err != nil {
		return err
	}
	defer crtOut.Close()
	if err := pem.Encode(crtOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		return err
	}

	// Attach CA certificate to generate complete chain if it's different from the cert data
	if cert != caCrt {
		if err := pem.Encode(crtOut, &pem.Block{Type: "CA CERTIFICATE", Bytes: caCrt.Raw}); err != nil {
			return err
		}
	}

	return nil
}

func createKey(keyPath string, key crypto.PrivateKey) error {
	// Create private key file
	keyOut, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer keyOut.Close()
	privBytes, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}

	err = pem.Encode(keyOut, &pem.Block{Type: "PRIVATE KEY", Bytes: privBytes})

	return err
}
