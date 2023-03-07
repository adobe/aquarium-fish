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

package util

import (
	"archive/tar"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/ulikunitz/xz"

	"github.com/adobe/aquarium-fish/lib/log"
)

// Stream function to download and unpack archive without
// using a storage file to make it as quick as possible
func DownloadUnpackArchive(url, out_dir, user, password string) error {
	log.Debug("Util: Downloading & Unpacking archive:", url)
	lock_path := out_dir + ".lock"

	// Wait for another process to download and unpack the archive
	// In case it failed to download - will be redownloaded further
	WaitLock(lock_path, func() {
		log.Debug("Util: Cleaning the abandoned files and begin redownloading:", out_dir)
		os.RemoveAll(out_dir)
	})

	if _, err := os.Stat(out_dir); !os.IsNotExist(err) {
		// The unpacked archive is already here, so nothing to do
		return nil
	}

	// Creating lock file in order to not screw it up in multiprocess system
	if err := CreateLock(lock_path); err != nil {
		return log.Error("Util: Unable to create lock file:", err)
	}
	defer os.Remove(lock_path)

	client := &http.Client{}
	req, _ := http.NewRequest("GET", url, nil)
	if user != "" && password != "" {
		req.SetBasicAuth(user, password)
	}
	resp, err := client.Do(req)
	if err != nil {
		os.RemoveAll(out_dir)
		return log.Error("Util: Unable to request url:", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		os.RemoveAll(out_dir)
		return log.Error("Util: Unable to download file:", url, resp.StatusCode, resp.Status)
	}

	// Printing the download progress
	bodypt := &PassThruMonitor{
		Reader: resp.Body,
		name:   fmt.Sprintf("Util: Downloading '%s'", out_dir),
		length: resp.ContentLength,
	}

	// Unpack the stream
	r, err := xz.NewReader(bodypt)
	if err != nil {
		os.RemoveAll(out_dir)
		return log.Error("Util: Unable to create XZ reader:", err)
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
			return log.Error("Util: Tar archive failed to iterate next file:", err)
		}

		// Check the name doesn't contain any traversal elements
		if strings.Contains(hdr.Name, "..") {
			os.RemoveAll(out_dir)
			return log.Error("Util: The archive filepath contains '..' which is security forbidden:", hdr.Name)
		}

		target := filepath.Join(out_dir, hdr.Name)

		switch hdr.Typeflag {
		case tar.TypeDir:
			// Create a directory
			err = os.MkdirAll(target, os.FileMode(hdr.Mode))
			if err != nil {
				os.RemoveAll(out_dir)
				return log.Error("Util: Unable to create directory:", target, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			// Write a file
			log.Debugf("Util: Extracting '%s': %s", out_dir, hdr.Name)
			err = os.MkdirAll(filepath.Dir(target), 0750)
			if err != nil {
				os.RemoveAll(out_dir)
				return log.Error("Util: Unable to create directory for file:", target, err)
			}
			w, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(hdr.Mode))
			if err != nil {
				os.RemoveAll(out_dir)
				return log.Error("Util: Unable to open file for unpack:", target, err)
			}
			defer w.Close()

			// TODO: Add in-stream sha256 calculation to verify against .sha256 file
			_, err = io.Copy(w, tr)
			if err != nil {
				os.RemoveAll(out_dir)
				return log.Error("Util: Unable to unpack content to file:", target, err)
			}
		}
	}

	return nil
}
