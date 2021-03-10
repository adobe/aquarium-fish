package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/adobe/aquarium-fish/lib/fish"
)

type MetaV1Processor struct {
	fish *fish.Fish
}

func checkIPv4Address(network *net.IPNet, ip net.IP) bool {
	fmt.Println("DEBUG: check network ip:", network.IP, len(network.IP))
	// Processing only networks we controlling (IPv4)
	// TODO: not 100% ensurance over the network control, but good enough for now
	if !bytes.HasSuffix(bytes.TrimRight(network.IP, " "), []byte(".1")) {
		return false
	}
	fmt.Println("DEBUG: passed suffix:", network)

	// Make sure checked IP is in the network
	if !network.Contains(ip) {
		return false
	}

	fmt.Println("DEBUG: passed address:", ip)
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
			fmt.Print(fmt.Errorf("Unable to get available addresses of the interfacei %s: %+v\n", i.Name, err.Error()))
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

func (e *MetaV1Processor) DataGetList(c *gin.Context) {
	var metadata map[string]interface{}

	res_int, _ := c.Get("resource")
	res, ok := res_int.(*fish.Resource)
	if !ok {
		log.Println("Fish API Meta: Unable to get resource from context")
		c.JSON(http.StatusNotFound, gin.H{"message": "No data found", "data": metadata})
		return
	}

	err := json.Unmarshal([]byte(res.Metadata), metadata)
	if err != nil {
		log.Println("Fish API Meta: Unable to parse metadata of resource", res.ID, res.Metadata)
		c.JSON(http.StatusNotFound, gin.H{"message": "Unable to parse metadata json"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "MetaData list", "data": metadata})
}

func (e *MetaV1Processor) DataGet(c *gin.Context) {
	//id := c.Param("key")
	c.JSON(http.StatusNotFound, gin.H{"message": "No data found"})
}
