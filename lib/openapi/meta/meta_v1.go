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

package meta

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/adobe/aquarium-fish/lib/fish"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
	"github.com/adobe/aquarium-fish/lib/util"
)

// H is a shortcut for map[string]interface{}
type H map[string]interface{}

type Processor struct {
	fish *fish.Fish
}

func NewV1Router(e *echo.Echo, fish *fish.Fish) {
	proc := &Processor{fish: fish}
	router := e.Group("")
	router.Use(
		// Only the local interface which we own can request
		proc.AddressAuth,
	)
	RegisterHandlers(router, proc)
}

func (e *Processor) AddressAuth(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		// Only the existing local resource access it's metadata
		res, err := e.fish.ResourceGetByIP(c.RealIP())
		if err != nil {
			log.Println("WARN: Unauthorized access to meta:", err)
			return echo.NewHTTPError(http.StatusUnauthorized, "Client IP was not found in the node Resources")
		}

		c.Set("resource", res)
		return next(c)
	}
}

func (e *Processor) Return(c echo.Context, code int, obj map[string]interface{}) error {
	format := c.QueryParam("format")
	if len(format) == 0 {
		format = "json"
	}
	prefix := c.QueryParam("prefix")

	data, err := util.SerializeMetadata(format, prefix, obj)
	if err != nil {
		return c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to format metadata: %v", err)})
	}

	mime := echo.MIMETextPlainCharsetUTF8
	if format == "json" {
		mime = echo.MIMEApplicationJSONCharsetUTF8
	}

	c.Blob(code, mime, data)

	return nil
}

func (e *Processor) DataGetList(c echo.Context, params types.DataGetListParams) error {
	var metadata map[string]interface{}

	res_int := c.Get("resource")
	res, ok := res_int.(*types.Resource)
	if !ok {
		e.Return(c, http.StatusNotFound, H{"message": "No data found"})
		return fmt.Errorf("Unable to get resource from context")
	}

	err := json.Unmarshal([]byte(res.Metadata), &metadata)
	if err != nil {
		e.Return(c, http.StatusNotFound, H{"message": "Unable to parse metadata json"})
		return fmt.Errorf("Unable to parse metadata of Resource: %s %s: %w", res.UID, res.Metadata, err)
	}

	return e.Return(c, http.StatusOK, metadata)
}

func (e *Processor) DataGet(c echo.Context, keyPath string, params types.DataGetParams) error {
	// TODO: implement it
	e.Return(c, http.StatusNotFound, H{"message": "TODO: Not implemented"})
	return fmt.Errorf("TODO: Not implemented")
}
