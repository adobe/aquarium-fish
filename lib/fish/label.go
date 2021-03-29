package fish

import (
	"errors"
	"time"

	"github.com/adobe/aquarium-fish/lib/util"
)

type Label struct {
	ID        int64 `gorm:"primaryKey"`
	CreatedAt time.Time

	Name       string            `json:"name" gorm:"uniqueIndex:idx_label_uniq"`    // Label name to find the proper one
	Version    int               `json:"version" gorm:"uniqueIndex:idx_label_uniq"` // Revision of the label
	Driver     string            `json:"driver"`                                    // Driver implements the label definition configuration
	Definition util.UnparsedJson `json:"definition"`                                // JSON-encoded definition for the driver
	Metadata   util.UnparsedJson `json:"metadata"`                                  // Additional metadata to the resource
}

func (f *Fish) LabelList() (labels []Label, err error) {
	err = f.db.Find(&labels).Error
	return labels, err
}

func (f *Fish) LabelCreate(l *Label) error {
	if l.Name == "" {
		return errors.New("Fish: Name can't be empty")
	}
	if l.Driver == "" {
		return errors.New("Fish: Driver can't be empty")
	}
	if l.Definition == "" {
		return errors.New("Fish: Definition can't be empty")
	}
	if l.Metadata == "" {
		l.Metadata = "{}"
	}

	return f.db.Create(l).Error
}

// Intentionally disabled - labels can be created once and can't be updated
// Create label with incremented version instead
/*func (f *Fish) LabelSave(label *Label) error {
	return f.db.Save(label).Error
}*/

func (f *Fish) LabelGet(id int64) (label *Label, err error) {
	label = &Label{}
	err = f.db.First(label, id).Error
	return label, err
}

func (f *Fish) LabelDelete(id int64) error {
	return f.db.Delete(&Label{}, id).Error
}
