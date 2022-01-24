package util

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/ulikunitz/xz"
)

// Stream function to download and unpack archive without
// using a storage file to make it as quick as possible
func DownloadUnpackArchive(url, out_dir, user, password string) error {
	log.Printf("Util: Downloading & Unpacking archive: %s\n", url)
	lock_path := out_dir + ".lock"

	// Wait for another process to download and unpack the archive
	// In case it failed to download - will be redownloaded further
	WaitLock(lock_path, func() {
		log.Println("Util: Cleaning the abandoned files and begin redownloading:", out_dir)
		os.RemoveAll(out_dir)
	})

	if _, err := os.Stat(out_dir); !os.IsNotExist(err) {
		// The unpacked archive is already here, so nothing to do
		return nil
	}

	// Creating lock file in order to not screw it up in multiprocess system
	if err := CreateLock(lock_path); err != nil {
		return err
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
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return errors.New(fmt.Sprintf("Util: Unable to download file: %s: %s", resp.Status, url))
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
		log.Println("Util: Unable to create XZ reader:", err)
		return err
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
			return err
		}

		target := filepath.Join(out_dir, hdr.Name)

		switch hdr.Typeflag {
		case tar.TypeDir:
			// Create a directory
			err = os.MkdirAll(target, os.FileMode(hdr.Mode))
			if err != nil {
				os.RemoveAll(out_dir)
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			// Write a file
			log.Printf("Extracting '%s': %s\n", out_dir, hdr.Name)
			w, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(hdr.Mode))
			if err != nil {
				os.RemoveAll(out_dir)
				return err
			}
			defer w.Close()

			// TODO: Add in-stream sha256 calculation to verify against .sha256 file
			_, err = io.Copy(w, tr)
			if err != nil {
				os.RemoveAll(out_dir)
				return err
			}
		}
	}

	return nil
}
