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
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Data for download/unpack tests
const test_image_ci_sha256 = "48975fe7070f46788898e0729067424a8426ab27fe424500c5046f730d8e2ea5"
const test_image_ci_path = `/aquarium/image/test/test-image-ci/test-image-ci-20230210.190425_ff1cd1cf.tar.xz`
const test_image_ci_data = `/Td6WFoAAATm1rRGBMDGBICgASEBHAAAAAAAAHVrYcHgT/8CPl0AOhlKzh19/6MaQrMr9RrXZZWk9zwPnxjjpSvabgz3XRQs+H+dqotO+/DDO4qGxBjzRCfdCYPLz7PwgesGWM6q2rgpyOodGy/fE8D+r8dfs91GlyBovVJc6uZdtbJKrWVnv+jyvbxH55bmsGT0bdLORrG6rcmHQZ8tRr3WakelitUHoo5AljY6fq9RGvSgoeCNlE5bs0W/yJSaxs+Au5fHr1UjwqaqkdobRwtLiDIkjVWx2VutgHqhVR5xKl1ZW01bzOSQqt+Ahqt4HS6ODgp3HQmKNRuIlJa2ydxxdVlZCE6QFngbcp0dyOboWbUTTNi26roufISGmRD2ZIfdnufbPi2Uk8o20R0gaGtVRo64+kBqukRvG9qb1+WvQuCaiJyYAZ9fvf5wGGOzsNERBVvUU0nMK058oqujolnNSlxnugsHj6FNY5PYBzzu31mKfqUQV95/OzsUKfNp8gcWSOj3L8TIzkxB2Njwu5iCFQ96qFBPw/ArUWlxhhQIWKCIOCdsvD4lGP/Pdk8XbZJnjCMV0f8TqsuKUKSzXxCf++3kyJw700Rx4ry2bAPLs0/qxNIsJfhors/MW0B0RrL3p7nLxGlcBCtP3vZZvqSNhPMhG3outPyPlD/bvHLAnQtJTtjphyU7UazpkjcXslP+bSei2X7/t9D4kVqZgasnpEEBpTay5d+n/TKHv9FxLhZWq4mglUsZ7RyNIg2wdJzpe/fJ9SwkQPVxw0q/e21FObbGiwsELvSMPr80buV3ecFzAAAAAMTNLJ0ukWt/AAHiBICgAQCOEmNAscRn+wIAAAAABFla`

var server *httptest.Server

func Test_image_validate(t *testing.T) {
	t.Run("good_url", func(t *testing.T) {
		image := Image{
			Url: "https://example.org/aquarium/image/test/test-image-ci/test-image-ci-20230210.190425_ff1cd1cf.tar.xz",
		}

		if err := image.Validate(); err != nil {
			t.Fatalf(`image.Validate() = %q, unexpected error: %v`, image.Url, err)
		}
		if image.Name != "test-image-ci" {
			t.Fatalf(`image.Validate() = %q, name is not equal the expected one: %v`, image.Url, image.Name)
		}
		if image.Version != "20230210.190425_ff1cd1cf" {
			t.Fatalf(`image.Validate() = %q, version is not equal the expected one: %v`, image.Url, image.Version)
		}
	})

	t.Run("bad_url_empty", func(t *testing.T) {
		image := Image{
			Url: "",
		}

		if err := image.Validate(); err == nil || err.Error() != "Image: Url is not provided" {
			t.Fatalf(`image.Validate() = %q, URL error expected, but incorrect was returned: %v`, image.Url, err)
		}
	})

	t.Run("bad_url_schema", func(t *testing.T) {
		image := Image{
			Url: "ftp://tst",
		}

		if err := image.Validate(); err == nil || err.Error() != `Image: Url schema is not supported: "ftp://tst"` {
			t.Fatalf(`image.Validate() = %q, URL error expected, but incorrect was returned: %v`, image.Url, err)
		}
	})

	t.Run("good_sum", func(t *testing.T) {
		image := Image{
			Url: "https://example.org/aquarium/image/test/test-image-ci/test-image-ci-20230210.190425_ff1cd1cf.tar.xz",
			Sum: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef",
		}

		if err := image.Validate(); err != nil {
			t.Fatalf(`image.Validate() = %q, unexpected error: %v`, image.Url, err)
		}
	})

	t.Run("badsum_algo", func(t *testing.T) {
		image := Image{
			Url: "https://example.org/aquarium/image/test/test-image-ci/test-image-ci-20230210.190425_ff1cd1cf.tar.xz",
			Sum: "incorrect:0123456789abcdef0123456789abcdef0123456789abcdef",
		}

		if err := image.Validate(); err == nil || err.Error() != `Image: Checksum with not supported algorithm (md5, sha1, sha256, sha512): "incorrect"` {
			t.Fatalf(`image.Validate() = %q, URL error expected, but incorrect was returned: %v`, image.Url, err)
		}
	})
}

func Test_image_downloadunpack(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "user" || password != "password" {
			w.Header().Set("WWW-Authenticate", `Basic realm="restricted", charset="UTF-8"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		if _, ok := r.URL.Query()["nosumheader"]; !ok {
			w.Header().Set("X-Checksum-Sha256", test_image_ci_sha256)
		}
		w.WriteHeader(http.StatusOK)
		data, _ := base64.StdEncoding.DecodeString(test_image_ci_data)
		w.Write(data)
	}
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch strings.TrimSpace(r.URL.Path) {
		case test_image_ci_path:
			handler(w, r)
		default:
			http.NotFoundHandler().ServeHTTP(w, r)
		}
	}))

	t.Run("good", func(t *testing.T) {
		image := Image{
			Url: server.URL + test_image_ci_path,
			Sum: "sha256:" + test_image_ci_sha256,
		}

		// Make sure image is ok
		if err := image.Validate(); err != nil {
			t.Fatalf(`image.Validate() = %q, unexpected error: %v`, image.Url, err)
		}

		// Download/unpack into temp directory
		if err := image.DownloadUnpack(t.TempDir(), "user", "password"); err != nil {
			t.Fatalf(`image.DownloadUnpack() = %q, unexpected error: %v`, image.Url, err)
		}
	})

	t.Run("bad_url", func(t *testing.T) {
		image := Image{
			Url: server.URL + "/not/existing/artifact-version.tar.xz",
			Sum: "sha256:" + test_image_ci_sha256,
		}

		// Make sure image is ok
		if err := image.Validate(); err != nil {
			t.Fatalf(`image.Validate() = %q, unexpected error: %v`, image.Url, err)
		}

		// Download/unpack into temp directory
		err := image.DownloadUnpack(t.TempDir(), "user", "password")
		if err == nil || err.Error() != `Image: Unable to download file "`+server.URL+`/not/existing/artifact-version.tar.xz": 404 Not Found` {
			t.Fatalf(`image.DownloadUnpack() = %q, error expected, but incorrect was returned: %v`, image.Url, err)
		}
	})

	t.Run("bad_header_checksum", func(t *testing.T) {
		image := Image{
			Url: server.URL + test_image_ci_path,
			Sum: "sha256:0123456789abcdef",
		}

		// Make sure image is ok
		if err := image.Validate(); err != nil {
			t.Fatalf(`image.Validate() = %q, unexpected error: %v`, image.Url, err)
		}

		// Download/unpack into temp directory
		err := image.DownloadUnpack(t.TempDir(), "user", "password")
		if err == nil || err.Error() != `Image: The remote checksum (from header X-Checksum-Sha256) doesn't equal the desired one: "`+test_image_ci_sha256+`" != "0123456789abcdef" for "`+server.URL+test_image_ci_path+`"` {
			t.Fatalf(`image.DownloadUnpack() = %q, error expected, but incorrect was returned: %v`, image.Url, err)
		}
	})

	t.Run("bad_calculated_checksum", func(t *testing.T) {
		image := Image{
			Url: server.URL + test_image_ci_path + "?nosumheader",
			Sum: "sha256:0123456789abcdef",
		}

		// Make sure image is ok
		if err := image.Validate(); err != nil {
			t.Fatalf(`image.Validate() = %q, unexpected error: %v`, image.Url, err)
		}

		// Download/unpack into temp directory
		err := image.DownloadUnpack(t.TempDir(), "user", "password")
		if err == nil || err.Error() != `Image: The calculated checksum doesn't equal the desired one: "`+test_image_ci_sha256+`" != "0123456789abcdef" for "`+server.URL+test_image_ci_path+`?nosumheader"` {
			t.Fatalf(`image.DownloadUnpack() = %q, error expected, but incorrect was returned: %v`, image.Url, err)
		}
	})
}
