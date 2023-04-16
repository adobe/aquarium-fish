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

package drivers

import (
	"archive/tar"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
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

// Image definition
type Image struct {
	Url string `json:"url"` // Address of the remote image to download it
	Sum string `json:"sum"` // Optional checksum of the image in format "<algo>:<checksum>"

	Name    string `json:"name"`    // Optional name of the image, if not set will use a part of the Url file name prior to last minus ("-") or ext
	Version string `json:"version"` // Optional version of the image, if not set will use a part of the Url file name after the last minus ("-") to ext

	Tag string `json:"tag"` // Optional identifier used by drivers to make sure the images will be processed properly
}

func (i *Image) Validate() error {
	// Check url is defined
	if i.Url == "" {
		return fmt.Errorf("Image: Url is not provided")
	}

	// Check url schema is supported
	if !(strings.HasPrefix(i.Url, "http://") || strings.HasPrefix(i.Url, "https://")) {
		return fmt.Errorf("Image: Url schema is not supported: %q", i.Url)
	}

	// Fill name out of image url
	if i.Name == "" {
		i.Name = path.Base(i.Url)
		minus_loc := strings.LastIndexByte(i.Name, '-')
		if minus_loc != -1 {
			// Use the part from beginnig to last minus ('-') - useful to separate version part
			i.Name = i.Name[0:minus_loc]
		} else if strings.LastIndexByte(i.Name, '.') != -1 {
			// Split by extension - need to take into account dual extension of tar archives (ex. ".tar.xz")
			name_split := strings.Split(i.Name, ".")
			if name_split[len(name_split)-2] == "tar" {
				i.Name = strings.Join(name_split[0:len(name_split)-2], ".")
			} else {
				i.Name = strings.Join(name_split[0:len(name_split)-1], ".")
			}
		}
	}

	// Fill version out of image url
	if i.Version == "" {
		i.Version = path.Base(i.Url)
		minus_loc := strings.LastIndexByte(i.Version, '-')
		if minus_loc != -1 {
			// Use the part from the last minus ('-') to the end
			i.Version = i.Version[minus_loc+1:]
		}
		if strings.LastIndexByte(i.Version, '.') != -1 {
			// Split by extension - need to take into account dual extension of tar archives (ex. ".tar.xz")
			version_split := strings.Split(i.Version, ".")
			if version_split[len(version_split)-2] == "tar" {
				i.Version = strings.Join(version_split[0:len(version_split)-2], ".")
			} else {
				i.Version = strings.Join(version_split[0:len(version_split)-1], ".")
			}
		}
	}

	// Check sum format
	if i.Sum != "" {
		sum_split := strings.SplitN(i.Sum, ":", 2)
		if len(i.Sum) > 0 && len(sum_split) != 2 {
			return fmt.Errorf("Image: Checksum should be in format '<algo>:<checksum>': %q", i.Sum)
		}
		algo := sum_split[0]
		if algo != "md5" && algo != "sha1" && algo != "sha256" && algo != "sha512" {
			return fmt.Errorf("Image: Checksum with not supported algorithm (md5, sha1, sha256, sha512): %q", algo)
		}
		if algo == "md5" || algo == "sha1" {
			log.Debug("Image: Insecure algorithm is used, please consider moving to sha256 or sha512:", algo)
		}
	}

	return nil
}

// Stream function to download and unpack image archive without using a storage file to make it as
// quick as possible.
// -> out_dir - is the directory where the image will be placed. It will be unpacked to out_dir/Name-Version/
// -> user, password - credentials for HTTP Basic auth
func (i *Image) DownloadUnpack(out_dir, user, password string) error {
	img_path := filepath.Join(out_dir, i.Name+"-"+i.Version)
	log.Debug("Image: Downloading & Unpacking image:", i.Url, img_path)
	lock_path := img_path + ".lock"

	// Wait for another process to download and unpack the archive
	// In case it failed to download - will be redownloaded further
	util.WaitLock(lock_path, func() {
		log.Debug("Util: Cleaning the abandoned files and begin redownloading:", img_path)
		os.RemoveAll(img_path)
	})

	if _, err := os.Stat(img_path); !os.IsNotExist(err) {
		// The unpacked archive is already here, so nothing to do
		return nil
	}

	// Creating lock file in order to not screw it up in multiprocess system
	if err := util.CreateLock(lock_path); err != nil {
		return fmt.Errorf("Util: Unable to create lock file: %v", err)
	}
	defer os.Remove(lock_path)

	client := &http.Client{}
	req, _ := http.NewRequest("GET", i.Url, nil)
	if user != "" && password != "" {
		req.SetBasicAuth(user, password)
	}
	resp, err := client.Do(req)
	if err != nil {
		os.RemoveAll(img_path)
		return fmt.Errorf("Image: Unable to request url %q: %v", i.Url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		os.RemoveAll(img_path)
		return fmt.Errorf("Image: Unable to download file %q: %s", i.Url, resp.Status)
	}

	// Printing the download progress
	bodypt := &util.PassThruMonitor{
		Reader: resp.Body,
		Name:   fmt.Sprintf("Image: Downloading '%s'", img_path),
		Length: resp.ContentLength,
	}

	// Process checksum
	var data_reader io.Reader
	var hasher hash.Hash
	if i.Sum == "" {
		// Just use the passthrough body as the source
		data_reader = bodypt
	} else {
		algo_sum := strings.SplitN(i.Sum, ":", 2)

		// Calculating checksum during reading from the body
		switch algo_sum[0] {
		case "md5":
			hasher = md5.New()
		case "sha1":
			hasher = sha1.New()
		case "sha256":
			hasher = sha256.New()
		case "sha512":
			hasher = sha512.New()
		default:
			os.RemoveAll(img_path)
			return fmt.Errorf("Image: Not recognized checksum algorithm (md5, sha1, sha256, sha512): %q", algo_sum[0])
		}

		data_reader = io.TeeReader(bodypt, hasher)

		// Check if headers contains the needed algo:hash for quick validation
		// We're not completely trust the server, but if it returns the wrong sum - we're dropping.
		// Header should look like: X-Checksum-Md5 X-Checksum-Sha1 X-Checksum-Sha256 (Artifactory)
		if remote_sum := resp.Header.Get("X-Checksum-" + strings.Title(algo_sum[0])); remote_sum != "" {
			// Server returned mathing header, so compare it's value to our checksum
			if remote_sum != algo_sum[1] {
				os.RemoveAll(img_path)
				return fmt.Errorf("Image: The remote checksum (from header X-Checksum-%s) doesn't equal the desired one: %q != %q for %q",
					strings.Title(algo_sum[0]), remote_sum, algo_sum[1], i.Url)
			}
		}
	}

	// Unpack the stream
	xzr, err := xz.NewReader(data_reader)
	if err != nil {
		os.RemoveAll(img_path)
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
			os.RemoveAll(img_path)
			return fmt.Errorf("Image: Tar archive failed to iterate next file: %v", err)
		}

		// Check the name doesn't contain any traversal elements
		if strings.Contains(hdr.Name, "..") {
			os.RemoveAll(img_path)
			return fmt.Errorf("Image: The archive filepath contains '..' which is security forbidden: %q", hdr.Name)
		}

		target := filepath.Join(img_path, hdr.Name)

		switch hdr.Typeflag {
		case tar.TypeDir:
			// Create a directory
			err = os.MkdirAll(target, os.FileMode(hdr.Mode))
			if err != nil {
				os.RemoveAll(img_path)
				return fmt.Errorf("Image: Unable to create directory %q: %v", target, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			// Write a file
			log.Debugf("Util: Extracting '%s': %s", img_path, hdr.Name)
			err = os.MkdirAll(filepath.Dir(target), 0750)
			if err != nil {
				os.RemoveAll(img_path)
				return fmt.Errorf("Image: Unable to create directory for file %q: %v", target, err)
			}
			w, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(hdr.Mode))
			if err != nil {
				os.RemoveAll(img_path)
				return fmt.Errorf("Image: Unable to open file %q for unpack: %v", target, err)
			}
			defer w.Close()

			// TODO: Add in-stream sha256 calculation for each file to verify against .sha256 data
			_, err = io.Copy(w, tr)
			if err != nil {
				os.RemoveAll(img_path)
				return fmt.Errorf("Image: Unable to unpack content to file %q: %v", target, err)
			}
		}
	}

	// Compare the calculated checksum to the desired one
	if i.Sum != "" {
		// Completing read of the stream to calculate the hash properly (tar will not do that)
		io.ReadAll(data_reader)

		algo_sum := strings.SplitN(i.Sum, ":", 2)
		calculated_sum := hex.EncodeToString(hasher.Sum(nil))
		if calculated_sum != algo_sum[1] {
			os.RemoveAll(img_path)
			return fmt.Errorf("Image: The calculated checksum doesn't equal the desired one: %q != %q for %q",
				calculated_sum, algo_sum[1], i.Url)
		}
	}

	return nil
}
