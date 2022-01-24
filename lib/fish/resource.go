package fish

import (
	"errors"
	"fmt"
	"log"
	"net"
	"strings"

	"github.com/mostlygeek/arp"
	"gorm.io/gorm"

	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

func (f *Fish) ResourceFind(filter *string) (rs []types.Resource, err error) {
	db := f.db
	if filter != nil {
		db = db.Where(*filter)
	}
	err = db.Find(&rs).Error
	return rs, err
}

func (f *Fish) ResourceListNode(node_id int64) (rs []types.Resource, err error) {
	err = f.db.Where("node_id = ?", node_id).Find(&rs).Error
	return rs, err
}

func (f *Fish) ResourceCreate(r *types.Resource) error {
	if len(r.HwAddr) == 0 {
		return errors.New("Fish: HwAddr can't be empty")
	}
	// TODO: check JSON
	if len(r.Metadata) < 2 {
		return errors.New("Fish: Metadata can't be empty")
	}
	return f.db.Create(r).Error
}

func (f *Fish) ResourceDelete(id int64) error {
	return f.db.Delete(&types.Resource{}, id).Error
}

func (f *Fish) ResourceSave(res *types.Resource) error {
	return f.db.Save(res).Error
}

func (f *Fish) ResourceGet(id int64) (res *types.Resource, err error) {
	res = &types.Resource{}
	err = f.db.First(res, id).Error
	return res, err
}

func fixHwAddr(hwaddr string) string {
	split := strings.Split(hwaddr, ":")
	if len(split) == 6 {
		// MAC address fix
		for i, v := range split {
			split[i] = fmt.Sprintf("%02s", v)
		}
		hwaddr = strings.Join(split, ":")
	}

	return hwaddr
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
		log.Print(fmt.Errorf("Unable to get the available network interfaces: %+v\n", err.Error()))
		return false
	}

	for _, i := range ifaces {
		addrs, err := i.Addrs()
		if err != nil {
			log.Print(fmt.Errorf("Unable to get available addresses of the interface %s: %+v\n", i.Name, err.Error()))
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

func (f *Fish) ResourceGetByIP(ip string) (res *types.Resource, err error) {
	res = &types.Resource{}

	// Check by IP first
	err = f.db.Where("node_id = ?", f.GetNodeID()).Where("ip_addr = ?", ip).First(res).Error
	if err == nil {
		// Check if the state is allocated to prevent old resources access
		if f.ApplicationIsAllocated(res.ApplicationID) != nil {
			return nil, errors.New("Fish: Prohibited to access the Resource of not allocated Application")
		}

		return res, nil
	}

	// Make sure the IP is the controlled network, otherwise someone from outside
	// could become a local node resource, so let's be careful
	if !isControlledNetwork(ip) {
		return nil, errors.New("Fish: Prohibited to serve the Resource IP from not controlled network")
	}

	// Check by MAC and update IP if found
	// need to fix due to on mac arp can return just one digit
	hw_addr := fixHwAddr(arp.Search(ip))
	if hw_addr == "" {
		return nil, gorm.ErrRecordNotFound
	}
	err = f.db.Where("node_id = ?", f.GetNodeID()).Where("hw_addr = ?", hw_addr).First(res).Error
	if err != nil {
		return nil, err
	}

	// Check if the state is allocated to prevent old resources access
	if f.ApplicationIsAllocated(res.ApplicationID) != nil {
		return nil, errors.New("Fish: Prohibited to access the Resource of not allocated Application")
	}

	log.Println("Fish: Update IP address for the Resource", res.ID, ip)
	res.IpAddr = ip
	err = f.ResourceSave(res)

	return res, err
}

func (f *Fish) ResourceGetByApplication(app_id int64) (res *types.Resource, err error) {
	res = &types.Resource{}
	err = f.db.Where("application_id = ?", app_id).First(res).Error
	return res, err
}

func (f *Fish) ResourceServiceMapping(res *types.Resource, dest string) string {
	sm := &types.ServiceMapping{}

	// Trying to find the record with Application and Location if possible
	// The application in priority, location - secondary priority, if no such
	// records found - default (ID 0) will be used
	err := f.db.Where(
		"application_id IN (?, 0)", res.ApplicationID).Where(
		"location_id IN (?, 0)", f.GetLocationID()).Where(
		"service = ?", dest).Order("application_id DESC").Order("location_id DESC").First(sm).Error
	if err != nil {
		return ""
	}

	return sm.Redirect
}
