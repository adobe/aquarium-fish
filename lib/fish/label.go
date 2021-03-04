package fish

import (
	"time"
)

type Label struct {
	ID        uint `gorm:"primaryKey"`
	CreatedAt time.Time
	UpdatedAt time.Time
	// Unable to use SoftDelete due to error during Save https://gorm.io/docs/delete.html#Soft-Delete

	Name       string // Label name to find the proper one
	Driver     string // Driver implements the label definition configuration
	Version    int    // Revision of the label
	Active     bool   // Is available to create new resources or not
	Definition string // JSON-encoded definition for the driver
}

func (e *App) LabelCreate(label *Label) error {
	return e.db.Create(label).Error
}

func (e *App) LabelSave(label *Label) error {
	return e.db.Save(label).Error
}

func (e *App) LabelGet(id int64) (label *Label, err error) {
	label = &Label{}
	err = e.db.First(label, id).Error
	return label, err
}
