package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/alessio/shellescape"
	"github.com/gin-gonic/gin"

	"git.corp.adobe.com/CI/aquarium-fish/lib/fish"
	"git.corp.adobe.com/CI/aquarium-fish/lib/util"
)

type MetaV1Processor struct {
	fish *fish.Fish
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

func (e *MetaV1Processor) AddressAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Only the controlled network IP's can get access to their meta
		if !isControlledNetwork(c.ClientIP()) {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		// Only the existing local resource
		res, err := e.fish.ResourceGetByIP(c.ClientIP())
		if err != nil {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		c.Set("resource", res)
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

func (e *MetaV1Processor) Return(c *gin.Context, code int, obj gin.H) {
	format := c.Request.URL.Query().Get("format")
	if len(format) == 0 {
		format = "json"
	}
	switch format {
	case "json": // Default json
		c.JSON(code, obj)
	case "yaml": // Regular yaml
		c.YAML(code, obj)
	case "env": // Plain format suitable to use in shell
		prefix := c.Request.URL.Query().Get("prefix")
		m := util.DotSerialize(prefix, obj)
		c.String(code, "")
		for key, val := range m {
			line := cleanShellKey(strings.Replace(shellescape.StripUnsafe(key), ".", "_", -1))
			if len(line) == 0 {
				continue
			}
			value := []byte("=" + shellescape.Quote(val) + "\n")
			c.Writer.Write(append(line, value...))
		}
	default:
		c.JSON(http.StatusBadRequest, gin.H{"message": "Incorrect `format` query param provided: " + format})
	}
}

func (e *MetaV1Processor) DataGetList(c *gin.Context) {
	var metadata map[string]interface{}

	res_int, _ := c.Get("resource")
	res, ok := res_int.(*fish.Resource)
	if !ok {
		log.Println("Fish API Meta: Unable to get resource from context")
		e.Return(c, http.StatusNotFound, gin.H{"message": "No data found", "data": metadata})
		return
	}

	err := json.Unmarshal([]byte(res.Metadata), &metadata)
	if err != nil {
		log.Println("Fish API Meta: Unable to parse metadata of resource", res.ID, res.Metadata, err)
		e.Return(c, http.StatusNotFound, gin.H{"message": "Unable to parse metadata json"})
		return
	}
	e.Return(c, http.StatusOK, gin.H{"message": "MetaData list", "data": metadata})
}

func (e *MetaV1Processor) DataGet(c *gin.Context) {
	//id := c.Param("key")
	e.Return(c, http.StatusNotFound, gin.H{"message": "No data found"})
}
