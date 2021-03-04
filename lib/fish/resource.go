package fish

import (
	"log"
	"time"

	"gorm.io/gorm"

	"github.com/mostlygeek/arp"
)

type Resource struct {
	ID        uint `gorm:"primaryKey"`
	CreatedAt time.Time
	UpdatedAt time.Time
	// Unable to use SoftDelete due to error during Save https://gorm.io/docs/delete.html#Soft-Delete

	Name     string // Used to identify resource by the requestor
	Node     Node   // Node that owns the resource
	NodeID   uint
	Label    Label // Label configuration which is defines the resource
	LabelID  uint
	IpAddr   string // IP Address of the resource to identify by the node
	HwAddr   string // MAC or any other network hardware address to identify incoming request
	Metadata string // Requestor metadata in JSON format
}

func (e *App) ResourceCreate(res *Resource) error {
	return e.db.Create(res).Error
}

func (e *App) ResourceSave(res *Resource) error {
	return e.db.Save(res).Error
}

func (e *App) ResourceGet(id int64) (res *Resource, err error) {
	res = &Resource{}
	err = e.db.First(res, id).Error
	return res, err
}

func (e *App) ResourceGetByIP(ip string) (res *Resource, err error) {
	// Check by IP first
	err = e.db.Where("node_id = ?", e.GetNodeID()).Where("ip_addr = ?", ip).First(res).Error
	if err == nil {
		return res, nil
	}

	// Check by MAC and update IP if found
	hw_addr := arp.Search(ip)
	if hw_addr == "" {
		return nil, gorm.ErrRecordNotFound
	}
	err = e.db.Where("node_id = ?", e.GetNodeID()).Where("hw_addr = ?", hw_addr).First(res).Error
	if err != nil {
		return nil, err
	}

	log.Println("Fish: Update IP address for the Resource", res.ID, ip)
	res.IpAddr = ip
	err = e.ResourceSave(res)

	return res, err
}
