package vmx

import (
	"encoding/json"
	"errors"
	"log"
)

/**
 * Definition example:
 *   image: macos-1015-ci-xcode122
 *   images:
 *     macos-1015: https://artifact-storage/aquarium/image/macos-1015-VERSION/macos-1015-VERSION.tar.xz
 *     macos-1015-ci: https://artifact-storage/aquarium/image/macos-1015-ci-VERSION/macos-1015-ci-VERSION.tar.xz
 *     macos-1015-ci-xcode122: https://artifact-storage/aquarium/image/macos-1015-ci-xcode122-VERSION/macos-1015-ci-xcode122-VERSION.tar.xz
 */
type Definition struct {
	Image  string            `json:"image"`  // Main image to use as reference
	Images map[string]string `json:"images"` // List of image dependencies
}

func (d *Definition) Apply(definition string) error {
	if err := json.Unmarshal([]byte(definition), d); err != nil {
		log.Println("VMX: Unable to apply the driver definition", err)
		return err
	}

	return d.Validate()
}

func (d *Definition) Validate() error {
	if d.Image == "" {
		return errors.New("VMX: No image is specified")
	}

	image_exist := false
	for name, url := range d.Images {
		if name == "" {
			return errors.New("VMX: No image name is specified")
		}
		if url == "" {
			return errors.New("VMX: No image url is specified")
		}
		if name == d.Image {
			image_exist = true
		}
	}
	if !image_exist {
		return errors.New("VMX: No image found in the images")
	}

	return nil
}
