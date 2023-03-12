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
	"testing"
)

func Test_image_validate_good_url(t *testing.T) {
	image := Image{
		Url: "https://example.org/aquarium/image/test/test-image-ci/test-image-ci-20230210.190425_ff1cd1cf.tar.xz",
	}

	if err := image.Validate(); err != nil {
		t.Fatalf(`image.Validate() = %q, unexpected error: %v`, image.Url, err)
	}
	if image.Name != "test-image-ci" {
		t.Fatalf(`image.Validate() = %q, name is not equal the expected one: %v`, image.Url, image.Name)
	}
}

func Test_image_validate_bad_url_empty(t *testing.T) {
	image := Image{
		Url: "",
	}

	if err := image.Validate(); err == nil || err.Error() != "Image: Url is not provided" {
		t.Fatalf(`image.Validate() = %q, URL error expected, but incorrect was returned: %v`, image.Url, err)
	}
}

func Test_image_validate_bad_url_schema(t *testing.T) {
	image := Image{
		Url: "ftp://tst",
	}

	if err := image.Validate(); err == nil || err.Error() != `Image: Url schema is not supported: "ftp://tst"` {
		t.Fatalf(`image.Validate() = %q, URL error expected, but incorrect was returned: %v`, image.Url, err)
	}
}

func Test_image_validate_good_sum(t *testing.T) {
	image := Image{
		Url: "https://example.org/aquarium/image/test/test-image-ci/test-image-ci-20230210.190425_ff1cd1cf.tar.xz",
		Sum: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef",
	}

	if err := image.Validate(); err != nil {
		t.Fatalf(`image.Validate() = %q, unexpected error: %v`, image.Url, err)
	}
}

func Test_image_validate_badsum_algo(t *testing.T) {
	image := Image{
		Url: "https://example.org/aquarium/image/test/test-image-ci/test-image-ci-20230210.190425_ff1cd1cf.tar.xz",
		Sum: "incorrect:0123456789abcdef0123456789abcdef0123456789abcdef",
	}

	if err := image.Validate(); err == nil || err.Error() != `Image: Checksum with not supported algorithm (md5, sha1, sha256, sha512): "incorrect"` {
		t.Fatalf(`image.Validate() = %q, URL error expected, but incorrect was returned: %v`, image.Url, err)
	}
}
