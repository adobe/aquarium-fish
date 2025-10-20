/**
 * Copyright 2021-2025 Adobe. All rights reserved.
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

package aquariumv2

import (
	"archive/tar"
	"context"
	"crypto/md5"  // #nosec G501
	"crypto/sha1" // #nosec G505
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/ulikunitz/xz"

	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/util"
)

// GetName returns a nice image name as a string or empty one if not set
func (i *Image) GetName() string {
	if i.Name == nil {
		return ""
	}
	return *i.Name
}

// GetVersion returns a nice image name as a string or empty one if not set
func (i *Image) GetVersion() string {
	if i.Version == nil {
		return ""
	}
	return *i.Version
}

// GetNameVersion returns the full name with version if it's defined
func (i *Image) GetNameVersion(separator string) string {
	if i.Name == nil {
		return ""
	}
	if i.Version != nil {
		return fmt.Sprintf("%s%s%s", *i.Name, separator, *i.Version)
	}
	return *i.Name
}

// GetURL returns a nice image url as a string or empty one if not set
func (i *Image) GetURL() string {
	if i.Url == nil {
		return ""
	}
	return *i.Url
}

// GetSum returns a nice image name as a string or empty one if not set
func (i *Image) GetSum() string {
	if i.Sum == nil {
		return ""
	}
	return *i.Sum
}

// GetTag returns a nice image name as a string or empty one if not set
func (i *Image) GetTag() string {
	if i.Tag == nil {
		return ""
	}
	return *i.Tag
}

// ToJSON converts image data to json format
func (i *Image) ToJSON() string {
	jsonData, err := json.Marshal(i)
	if err != nil {
		return ""
	}
	return string(jsonData)
}

// Validate makes sure the image spec is good enough
func (i *Image) Validate() error {
	// Check url is defined
	if i.Url != nil && *i.Url != "" {
		// Ensure url schema is supported
		if !(strings.HasPrefix(*i.Url, "http://") || strings.HasPrefix(*i.Url, "https://")) {
			return fmt.Errorf("Image: Url schema is not supported: %q", *i.Url)
		}

		// Fill name out of image url
		if i.Name == nil || *i.Name == "" {
			name := path.Base(*i.Url)
			minusLoc := strings.LastIndexByte(name, '-')
			if minusLoc != -1 {
				// Use the part from beginning to last minus ('-') - useful to separate version part
				name = name[0:minusLoc]
			} else if strings.LastIndexByte(name, '.') != -1 {
				// Split by extension - need to take into account dual extension of tar archives (ex. ".tar.xz")
				nameSplit := strings.Split(name, ".")
				if nameSplit[len(nameSplit)-2] == "tar" {
					name = strings.Join(nameSplit[0:len(nameSplit)-2], ".")
				} else {
					name = strings.Join(nameSplit[0:len(nameSplit)-1], ".")
				}
			}
			i.Name = &name
		}

		// Fill version out of image url
		if i.Version == nil || *i.Version == "" {
			version := path.Base(*i.Url)
			minusLoc := strings.LastIndexByte(version, '-')
			if minusLoc != -1 {
				// Use the part from the last minus ('-') to the end
				version = version[minusLoc+1:]
			}
			if strings.LastIndexByte(version, '.') != -1 {
				// Split by extension - need to take into account dual extension of tar archives (ex. ".tar.xz")
				versionSplit := strings.Split(version, ".")
				if versionSplit[len(versionSplit)-2] == "tar" {
					version = strings.Join(versionSplit[0:len(versionSplit)-2], ".")
				} else {
					version = strings.Join(versionSplit[0:len(versionSplit)-1], ".")
				}
			}
			i.Version = &version
		}
	} else {
		// If URL is not provided - name should be here for sure
		if i.Name == nil || *i.Name == "" {
			return fmt.Errorf("Image: Url or Name should be provided")
		}
	}

	// Check sum format
	if i.Sum != nil && *i.Sum != "" {
		sumSplit := strings.SplitN(*i.Sum, ":", 2)
		if len(sumSplit) != 2 {
			return fmt.Errorf("Image: Checksum should be in format '<algo>:<checksum>': %q", *i.Sum)
		}
		algo := sumSplit[0]
		if algo != "md5" && algo != "sha1" && algo != "sha256" && algo != "sha512" {
			return fmt.Errorf("Image: Checksum with not supported algorithm (md5, sha1, sha256, sha512): %q", algo)
		}
		if algo == "md5" || algo == "sha1" {
			log.WithFunc("provider", "Validate").Warn("Insecure algorithm is used, please consider moving to sha256 or sha512", "algo", algo)
		}
	}

	return nil
}

// DownloadUnpack is a stream function to download and unpack image archive without using a storage file to make it as
// quick as possible.
// -> out_dir - is the directory where the image will be placed. It will be unpacked to out_dir/Name-Version/
// -> user, password - credentials for HTTP Basic auth
func (i *Image) DownloadUnpack(outDir, user, password string) error {
	logger := log.WithFunc("provider", "DownloadUnpack")
	if i.Name == nil || i.Version == nil || i.Url == nil {
		return fmt.Errorf("Image: Name, Version or URL of Image are not set")
	}
	imgPath := filepath.Join(outDir, fmt.Sprintf("%s-%s", *i.Name, *i.Version))
	logger.Debug("Downloading & Unpacking image", "img_url", *i.Url, "img_path", imgPath)
	lockPath := imgPath + ".lock"

	// Wait for another process to download and unpack the archive
	// In case it failed to download - will be redownloaded further
	util.WaitLock(lockPath, func() {
		logger.Debug("Cleaning the abandoned files and begin redownloading", "img_path", imgPath)
		os.RemoveAll(imgPath)
	})

	if _, err := os.Stat(imgPath); !os.IsNotExist(err) {
		// The unpacked archive is already here, so nothing to do
		return nil
	}

	// Creating lock file in order to not screw it up in multiprocess system
	if err := util.CreateLock(lockPath); err != nil {
		return fmt.Errorf("Util: Unable to create lock file: %v", err)
	}
	defer os.Remove(lockPath)

	client := &http.Client{}
	req, _ := http.NewRequestWithContext(context.TODO(), http.MethodGet, *i.Url, nil)
	if user != "" && password != "" {
		req.SetBasicAuth(user, password)
	}
	resp, err := client.Do(req)
	if err != nil {
		os.RemoveAll(imgPath)
		return fmt.Errorf("Image: Unable to request url %q: %v", *i.Url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		os.RemoveAll(imgPath)
		return fmt.Errorf("Image: Unable to download file %q: %s", *i.Url, resp.Status)
	}

	// Printing the download progress
	bodypt := &util.PassThruMonitor{
		Reader: resp.Body,
		Name:   fmt.Sprintf("Image: Downloading '%s'", imgPath),
		Length: resp.ContentLength,
	}

	// Process checksum
	var dataReader io.Reader
	var hasher hash.Hash
	if i.Sum == nil || *i.Sum == "" {
		// Just use the passthrough body as the source
		dataReader = bodypt
	} else {
		algoSum := strings.SplitN(*i.Sum, ":", 2)

		// Calculating checksum during reading from the body
		switch algoSum[0] {
		case "md5":
			hasher = md5.New() // #nosec G401
		case "sha1":
			hasher = sha1.New() // #nosec G401
		case "sha256":
			hasher = sha256.New()
		case "sha512":
			hasher = sha512.New()
		default:
			os.RemoveAll(imgPath)
			return fmt.Errorf("Image: Not recognized checksum algorithm (md5, sha1, sha256, sha512): %q", algoSum[0])
		}

		dataReader = io.TeeReader(bodypt, hasher)

		// Check if headers contains the needed algo:hash for quick validation
		// We're not completely trust the server, but if it returns the wrong sum - we're dropping.
		// Header should look like: X-Checksum-Md5 X-Checksum-Sha1 X-Checksum-Sha256 (Artifactory)
		if remoteSum := resp.Header.Get("X-Checksum-" + strings.Title(algoSum[0])); remoteSum != "" { //nolint:staticcheck // SA1019 Strictly ASCII here
			// Server returned mathing header, so compare it's value to our checksum
			if remoteSum != algoSum[1] {
				os.RemoveAll(imgPath)
				return fmt.Errorf("Image: The remote checksum (from header X-Checksum-%s) doesn't equal the desired one: %q != %q for %q",
					strings.Title(algoSum[0]), remoteSum, algoSum[1], *i.Url) //nolint:staticcheck // SA1019 Strictly ASCII here
			}
		}
	}

	// Unpack the stream
	xzr, err := xz.NewReader(dataReader)
	if err != nil {
		os.RemoveAll(imgPath)
		return fmt.Errorf("Image: Unable to create XZ reader: %v", err)
	}

	// Untar the stream
	// Create a tar Reader
	tr := tar.NewReader(xzr)

	// Iterate through the files in the archive.
	for {
		hdr, err := tr.Next()
		if err == io.EOF { // End of the tar archive
			break
		}
		if err != nil {
			os.RemoveAll(imgPath)
			return fmt.Errorf("Image: Tar archive failed to iterate next file: %v", err)
		}

		// Check the name doesn't contain any traversal elements
		if strings.Contains(hdr.Name, "..") {
			os.RemoveAll(imgPath)
			return fmt.Errorf("Image: The archive filepath contains '..' which is security forbidden: %q", hdr.Name)
		}

		target := filepath.Join(imgPath, hdr.Name) // #nosec G305 , checked above

		switch hdr.Typeflag {
		case tar.TypeDir:
			// Create a directory
			err = os.MkdirAll(target, os.FileMode(hdr.Mode))
			if err != nil {
				os.RemoveAll(imgPath)
				return fmt.Errorf("Image: Unable to create directory %q: %v", target, err)
			}
		case tar.TypeReg:
			// Write a file
			logger.Debug("Extracting", "img_path", imgPath, "name", hdr.Name)
			err = os.MkdirAll(filepath.Dir(target), 0750)
			if err != nil {
				os.RemoveAll(imgPath)
				return fmt.Errorf("Image: Unable to create directory for file %q: %v", target, err)
			}
			w, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(hdr.Mode))
			if err != nil {
				os.RemoveAll(imgPath)
				return fmt.Errorf("Image: Unable to open file %q for unpack: %v", target, err)
			}

			// TODO: Add in-stream sha256 calculation for each file to verify against .sha256 data
			for {
				_, err = io.CopyN(w, tr, 8196)
				if err == nil {
					continue
				} else if err == io.EOF {
					break
				}
				os.RemoveAll(imgPath)
				w.Close()
				return fmt.Errorf("Image: Unable to unpack content to file %q: %v", target, err)
			}
			w.Close()
		}
	}

	// Compare the calculated checksum to the desired one
	if i.Sum != nil && *i.Sum != "" {
		// Completing read of the stream to calculate the hash properly (tar will not do that)
		io.ReadAll(dataReader)

		algoSum := strings.SplitN(*i.Sum, ":", 2)
		calculatedSum := hex.EncodeToString(hasher.Sum(nil))
		if calculatedSum != algoSum[1] {
			os.RemoveAll(imgPath)
			return fmt.Errorf("Image: The calculated checksum doesn't equal the desired one: %q != %q for %q",
				calculatedSum, algoSum[1], *i.Url)
		}
	}

	return nil
}
