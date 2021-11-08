package meta

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/alessio/shellescape"
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
			return echo.NewHTTPError(http.StatusUnauthorized, "Client IP was not found in the node Resources")
		}

		c.Set("resource", res)
		return next(c)
	}
}

func cleanShellKey(in string) []byte {
	s := []byte(in)
	j := 0
	for _, b := range s {
		if j == 0 && ('0' <= b && b <= '9') {
			// Skip first numeric symbols
			continue
		}
		if ('a' <= b && b <= 'z') || ('A' <= b && b <= 'Z') || ('0' <= b && b <= '9') || b == '_' {
			s[j] = b
			j++
		}
	}
	return s[:j]
}

func (e *Processor) Return(c echo.Context, code int, obj map[string]interface{}) error {
	format := c.QueryParam("format")
	if len(format) == 0 {
		format = "json"
	}
	switch format {
	case "json": // Default json
		return c.JSON(code, obj)
	case "env": // Plain format suitable to use in shell
		prefix := c.QueryParam("prefix")
		m := util.DotSerialize(prefix, obj)
		c.String(code, "")
		for key, val := range m {
			line := cleanShellKey(strings.Replace(shellescape.StripUnsafe(key), ".", "_", -1))
			if len(line) == 0 {
				continue
			}
			value := []byte("=" + shellescape.Quote(val) + "\n")
			c.Response().Write(append(line, value...))
		}
		return nil
	case "ps1": // Plain format suitable to use in powershell
		prefix := c.QueryParam("prefix")
		m := util.DotSerialize(prefix, obj)
		c.String(code, "")
		for key, val := range m {
			line := cleanShellKey(strings.Replace(shellescape.StripUnsafe(key), ".", "_", -1))
			if len(line) == 0 {
				continue
			}
			// Shell quote is not applicable here, so using the custom one
			value := []byte("='" + strings.Replace(val, "'", "''", -1) + "'\n")
			c.Response().Write(append([]byte("$"), append(line, value...)...))
		}
		return nil
	default:
		return c.JSON(http.StatusBadRequest, H{"message": "Incorrect `format` query param provided: " + format})
	}
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
		return fmt.Errorf("Unable to parse metadata of resource: %d %s: %w", res.ID, res.Metadata, err)
	}

	return e.Return(c, http.StatusOK, metadata)
}

func (e *Processor) DataGet(c echo.Context, keyPath string, params types.DataGetParams) error {
	// TODO: implement it
	e.Return(c, http.StatusNotFound, H{"message": "No data found"})
	return fmt.Errorf("TODO: Not implemented")
}
