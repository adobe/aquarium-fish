package fish

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/mostlygeek/arp"
	"gorm.io/gorm"

	"github.com/adobe/aquarium-fish/lib/util"
)

type Resource struct {
	ID        int64 `gorm:"primaryKey"`
	CreatedAt time.Time
	UpdatedAt time.Time

	ApplicationID int64        `json:"application_id"`
	Application   *Application `json:"-"` // Resource request from the user

	NodeID int64 `json:"node_id"`
	Node   *Node `json:"-"` // Node that owns the resource

	IpAddr   string            `json:"ip_addr"`  // IP Address of the resource to identify by the node
	HwAddr   string            `json:"hw_addr"`  // MAC or any other network hardware address to identify incoming request
	Metadata util.UnparsedJson `json:"metadata"` // Combined metadata (Request + Driver)
}

func (f *Fish) ResourceFind(filter string) (rs []Resource, err error) {
	err = f.db.Where(filter).Find(&rs).Error
	return rs, err
}

func (f *Fish) ResourceListNode(node_id int64) (rs []Resource, err error) {
	err = f.db.Where("node_id = ?", node_id).Find(&rs).Error
	return rs, err
}

func (f *Fish) ResourceCreate(r *Resource) error {
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
	return f.db.Delete(&Resource{}, id).Error
}

func (f *Fish) ResourceSave(res *Resource) error {
	return f.db.Save(res).Error
}

func (f *Fish) ResourceGet(id int64) (res *Resource, err error) {
	res = &Resource{}
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

func (f *Fish) ResourceGetByIP(ip string) (res *Resource, err error) {
	res = &Resource{}

	// Check by IP first
	err = f.db.Where("node_id = ?", f.GetNodeID()).Where("ip_addr = ?", ip).First(res).Error
	if err == nil {
		return res, nil
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

	log.Println("Fish: Update IP address for the Resource", res.ID, ip)
	res.IpAddr = ip
	err = f.ResourceSave(res)

	return res, err
}

func (f *Fish) ResourceGetByApplication(app_id int64) (res *Resource, err error) {
	res = &Resource{}
	err = f.db.Where("application_id = ?", app_id).First(res).Error
	return res, err
}
