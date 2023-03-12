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
	Name string `json:"name"` // Optional name of the image, if not set will use a part of the Url file name prior to last minus ("-") or ext
	Url  string `json:"url"`  // Address of the remote image to download it
	Sum  string `json:"sum"`  // Optional checksum of the image in format "<algo>:<checksum>"
}

func (i *Image) Validate() error {
	// Check if url is defined
	if len(i.Url) < 1 {
		return fmt.Errorf("Image: Url is not provided")
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

	// Check sum format
	if i.Sum != "" {
		sum_split := strings.SplitN(i.Sum, ":", 2)
		if len(i.Sum) > 0 && len(sum_split) != 2 {
			return log.Error("Image: Checksum should be in format '<algo>:<checksum>':", i.Sum)
		}
		algo := sum_split[0]
		if algo != "md5" && algo != "sha1" && algo != "sha256" && algo != "sha512" {
			return log.Error("Image: Checksum with not supported algorithm (md5, sha1, sha256, sha512):", algo)
		}
		if algo == "md5" || algo == "sha1" {
			log.Debug("Image: Insecure algorithm is used, please consider moving to sha256 or sha512:", algo)
		}
	}

	return nil
}

// Stream function to download and unpack image archive without
// using a storage file to make it as quick as possible
func (i *Image) DownloadUnpack(out_dir, user, password string) error {
	log.Debug("Image: Downloading & Unpacking image:", i.Url)
	lock_path := out_dir + ".lock"

	// Wait for another process to download and unpack the archive
	// In case it failed to download - will be redownloaded further
	util.WaitLock(lock_path, func() {
		log.Debug("Util: Cleaning the abandoned files and begin redownloading:", out_dir)
		os.RemoveAll(out_dir)
	})

	if _, err := os.Stat(out_dir); !os.IsNotExist(err) {
		// The unpacked archive is already here, so nothing to do
		return nil
	}

	// Creating lock file in order to not screw it up in multiprocess system
	if err := util.CreateLock(lock_path); err != nil {
		return log.Error("Util: Unable to create lock file:", err)
	}
	defer os.Remove(lock_path)

	client := &http.Client{}
	req, _ := http.NewRequest("GET", i.Url, nil)
	if user != "" && password != "" {
		req.SetBasicAuth(user, password)
	}
	resp, err := client.Do(req)
	if err != nil {
		os.RemoveAll(out_dir)
		return log.Error("Image: Unable to request url:", i.Url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		os.RemoveAll(out_dir)
		return log.Error("Image: Unable to download file:", i.Url, resp.StatusCode, resp.Status)
	}

	// Printing the download progress
	bodypt := &util.PassThruMonitor{
		Reader: resp.Body,
		Name:   fmt.Sprintf("Image: Downloading '%s'", out_dir),
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
			os.RemoveAll(out_dir)
			return log.Error("Image: Not recognized checksum algorithm (md5, sha1, sha256, sha512):", algo_sum[0])
		}

		data_reader = io.TeeReader(bodypt, hasher)

		// Check if headers contains the needed algo:hash for quick validation
		// We're trust the server and if it returns not matching checksum - we're dropping the ball
		// Header should look like: X-Checksum-Md5 X-Checksum-Sha1 X-Checksum-Sha256 (Artifactory)
		if remote_sum := resp.Header.Get("X-Checksum-" + strings.Title(algo_sum[0])); remote_sum != "" {
			// Server returned mathing header, so compare it's value to our checksum
			if remote_sum != algo_sum[1] {
				os.RemoveAll(out_dir)
				return log.Errorf("Image: The remote checksum (from header X-Checksum-%s) doesn't equal the desired one: %q != %q for %q",
					strings.Title(algo_sum[0]), remote_sum, algo_sum[1], i.Url)
			}
		}
	}

	// Unpack the stream
	r, err := xz.NewReader(data_reader)
	if err != nil {
		os.RemoveAll(out_dir)
		return log.Error("Image: Unable to create XZ reader:", err)
	}

	// Untar the stream
	// Create a tar Reader
	tr := tar.NewReader(r)

	// Iterate through the files in the archive.
	for {
		hdr, err := tr.Next()
		if err == io.EOF { // End of the tar archive
			break
		}
		if err != nil {
			os.RemoveAll(out_dir)
			return log.Error("Image: Tar archive failed to iterate next file:", err)
		}

		// Check the name doesn't contain any traversal elements
		if strings.Contains(hdr.Name, "..") {
			os.RemoveAll(out_dir)
			return log.Error("Image: The archive filepath contains '..' which is security forbidden:", hdr.Name)
		}

		target := filepath.Join(out_dir, hdr.Name)

		switch hdr.Typeflag {
		case tar.TypeDir:
			// Create a directory
			err = os.MkdirAll(target, os.FileMode(hdr.Mode))
			if err != nil {
				os.RemoveAll(out_dir)
				return log.Error("Image: Unable to create directory:", target, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			// Write a file
			log.Debugf("Util: Extracting '%s': %s", out_dir, hdr.Name)
			err = os.MkdirAll(filepath.Dir(target), 0750)
			if err != nil {
				os.RemoveAll(out_dir)
				return log.Error("Image: Unable to create directory for file:", target, err)
			}
			w, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(hdr.Mode))
			if err != nil {
				os.RemoveAll(out_dir)
				return log.Error("Image: Unable to open file for unpack:", target, err)
			}
			defer w.Close()

			// TODO: Add in-stream sha256 calculation for each file to verify against .sha256 data
			_, err = io.Copy(w, tr)
			if err != nil {
				os.RemoveAll(out_dir)
				return log.Error("Image: Unable to unpack content to file:", target, err)
			}
		}
	}

	// Compare the calculated checksum to the desired one
	if i.Sum != "" {
		algo_sum := strings.SplitN(i.Sum, ":", 2)
		calculated_sum := hex.EncodeToString(hasher.Sum(nil))
		if calculated_sum != algo_sum[1] {
			os.RemoveAll(out_dir)
			return log.Errorf("Image: The calculated checksum doesn't equal the desired one: %q != %q for %q",
				calculated_sum, algo_sum[1], i.Url)
		}
	}

	return nil
}
