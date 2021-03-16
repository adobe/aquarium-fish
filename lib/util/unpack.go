package util

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/xi2/xz"
)

func Unpack(path string, outdir string) error {
	// Open a file
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// Create an xz Reader
	r, err := xz.NewReader(f, 0)
	if err != nil {
		return err
	}

	// Create a tar Reader
	tr := tar.NewReader(r)

	// Iterate through the files in the archive.
	for {
		hdr, err := tr.Next()
		if err == io.EOF { // End of the tar archive
			break
		}
		if err != nil {
			return err
		}

		target := filepath.Join(outdir, hdr.Name)

		switch hdr.Typeflag {
		case tar.TypeDir:
			// Create a directory
			err = os.MkdirAll(target, 0755)
			if err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			// Write a file
			fmt.Println("VMX: Extracting image file:", hdr.Name)
			w, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			defer w.Close()

			_, err = io.Copy(w, tr)
			if err != nil {
				return err
			}
		}
	}

	return nil
}
