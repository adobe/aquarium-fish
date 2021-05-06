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

func InitTlsPairCa(hosts []string, ca_path, key_path, crt_path string) error {
	// Generates simple CA and Node certificate signed by the CA
	_, ca_err := os.Stat(ca_path)
	if os.IsNotExist(ca_err) {
		// Generate new CA since it's not exist
		if err := generateSimpleCa(getCaKeyFromCertPath(ca_path), ca_path); err != nil {
			return err
		}
	}

	_, key_err := os.Stat(key_path)
	_, crt_err := os.Stat(crt_path)
	if os.IsNotExist(key_err) || os.IsNotExist(crt_err) {
		// Generate fish key & cert
		if err := generateSimpleKeyCert(hosts, key_path, crt_path, ca_path); err != nil {
			return err
		}
	}

	return nil
}

func getCaKeyFromCertPath(ca_path string) string {
	// Just trim the name extension and add ".key"
	filename := filepath.Base(ca_path)
	n := strings.LastIndexByte(filename, '.')
	if n == -1 {
		return ca_path
	}
	return filepath.Join(filepath.Dir(ca_path), filename[:n]+".key")
}

func generateSimpleCa(key_path, crt_path string) error {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}

	not_before := time.Now()

	serial_number_limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial_number, err := rand.Int(rand.Reader, serial_number_limit)
	if err != nil {
		return err
	}

	template := x509.Certificate{
		SerialNumber: serial_number,
		Subject: pkix.Name{
			// It's just an example CA - for prod generate CA & certs yourself with openssl
			Organization: []string{"Example Co CA"},
		},

		NotBefore: not_before,
		NotAfter:  not_before.AddDate(10, 0, 0), // 10y

		IsCA:     true,
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,

		BasicConstraintsValid: true,
	}

	// Generate certificate
	if err := createCert(crt_path, &priv.PublicKey, priv, &template, &template); err != nil {
		return err
	}

	// Create private key file
	if err := createKey(key_path, priv); err != nil {
		return err
	}

	return nil
}

func generateSimpleKeyCert(hosts []string, key_path, crt_path, ca_path string) error {
	// Load the CA key and cert
	ca_tls, err := tls.LoadX509KeyPair(ca_path, getCaKeyFromCertPath(ca_path))
	if err != nil {
		return err
	}
	ca_key := ca_tls.PrivateKey

	ca_crt, err := x509.ParseCertificate(ca_tls.Certificate[0])
	if err != nil {
		return err
	}

	// Generate simple service sertificate
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}

	not_before := time.Now()

	serial_number_limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial_number, err := rand.Int(rand.Reader, serial_number_limit)
	if err != nil {
		return err
	}

	template := x509.Certificate{
		SerialNumber: serial_number,
		Subject: pkix.Name{
			Organization: []string{"Example Co Crt"},
		},

		NotBefore: not_before,
		NotAfter:  not_before.AddDate(1, 0, 0), // 1y

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
	if err := createCert(crt_path, &priv.PublicKey, ca_key, &template, ca_crt); err != nil {
		return err
	}

	// Create private key file
	if err := createKey(key_path, priv); err != nil {
		return err
	}

	return nil
}

func createCert(crt_path string, pubkey crypto.PublicKey, ca_key crypto.PrivateKey, cert, ca_crt *x509.Certificate) error {
	// Generate certificate
	der_bytes, err := x509.CreateCertificate(rand.Reader, cert, ca_crt, pubkey, ca_key)
	if err != nil {
		return err
	}

	// Create certificate file
	crt_out, err := os.Create(crt_path)
	if err != nil {
		return err
	}
	defer crt_out.Close()
	if err := pem.Encode(crt_out, &pem.Block{Type: "CERTIFICATE", Bytes: der_bytes}); err != nil {
		return err
	}

	// Attach CA certificate to generate complete chain
	if err := pem.Encode(crt_out, &pem.Block{Type: "CA CERTIFICATE", Bytes: ca_crt.Raw}); err != nil {
		return err
	}

	return nil
}

func createKey(key_path string, key crypto.PrivateKey) error {
	// Create private key file
	key_out, err := os.OpenFile(key_path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer key_out.Close()
	priv_bytes, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	if err := pem.Encode(key_out, &pem.Block{Type: "PRIVATE KEY", Bytes: priv_bytes}); err != nil {
		return err
	}

	return nil
}
