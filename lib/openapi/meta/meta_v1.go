package meta

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/alessio/shellescape"
	"github.com/labstack/echo/v4"

	"git.corp.adobe.com/CI/aquarium-fish/lib/fish"
	"git.corp.adobe.com/CI/aquarium-fish/lib/openapi/types"
	"git.corp.adobe.com/CI/aquarium-fish/lib/util"
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

func checkIPv4Address(network *net.IPNet, ip net.IP) bool {
	// Processing only networks we controlling (IPv4)
	// TODO: not 100% ensurance over the network control, but good enough for now
	if !strings.HasSuffix(network.IP.String(), ".1") {
		return false
	}

	// Make sure checked IP is in the network
	if !network.Contains(ip) {
		return false
	}

	return true
}

func isControlledNetwork(ip string) bool {
	// Relatively long process executed for each request, but gives us flexibility
	// TODO: Could be optimized to collect network data on start or periodically
	ip_parsed := net.ParseIP(ip)

	ifaces, err := net.Interfaces()
	if err != nil {
		fmt.Print(fmt.Errorf("Unable to get the available network interfaces: %+v\n", err.Error()))
		return false
	}

	for _, i := range ifaces {
		addrs, err := i.Addrs()
		if err != nil {
			fmt.Print(fmt.Errorf("Unable to get available addresses of the interface %s: %+v\n", i.Name, err.Error()))
			continue
		}

		for _, a := range addrs {
			switch v := a.(type) {
			case *net.IPNet:
				if checkIPv4Address(v, ip_parsed) {
					return true
				}
			}
		}
	}
	return false
}

func (e *Processor) AddressAuth(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		// Only the controlled network IP's can get access to their meta
		if !isControlledNetwork(c.RealIP()) {
			return echo.NewHTTPError(http.StatusUnauthorized, "Client IP is from not controlled network")
		}

		// Only the existing local resource
		res, err := e.fish.ResourceGetByIP(c.RealIP())
		if err != nil {
			return echo.NewHTTPError(http.StatusUnauthorized, "Client IP was not found in the Resources")
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
	default:
		return c.JSON(http.StatusBadRequest, H{"message": "Incorrect `format` query param provided: " + format})
	}
}

func (e *Processor) DataGetList(c echo.Context, params types.DataGetListParams) error {
	var metadata map[string]interface{}

	res_int := c.Get("resource")
	res, ok := res_int.(*types.Resource)
	if !ok {
		e.Return(c, http.StatusNotFound, H{"message": "No data found", "data": metadata})
		return fmt.Errorf("Unable to get resource from context")
	}

	err := json.Unmarshal([]byte(res.Metadata), &metadata)
	if err != nil {
		e.Return(c, http.StatusNotFound, H{"message": "Unable to parse metadata json"})
		return fmt.Errorf("Unable to parse metadata of resource: %d %s: %w", res.ID, res.Metadata, err)
	}

	return e.Return(c, http.StatusOK, H{"message": "MetaData list", "data": metadata})
}

func (e *Processor) DataGet(c echo.Context, keyPath string, params types.DataGetParams) error {
	// TODO: implement it
	e.Return(c, http.StatusNotFound, H{"message": "No data found"})
	return fmt.Errorf("TODO: Not implemented")
}
