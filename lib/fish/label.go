package fish

import (
	"errors"
	"time"
)

type Label struct {
	ID        int64 `gorm:"primaryKey"`
	CreatedAt time.Time

	Name       string          `json:"name" gorm:"uniqueIndex:idx_label_uniq"`    // Label name to find the proper one
	Version    int             `json:"version" gorm:"uniqueIndex:idx_label_uniq"` // Revision of the label
	Driver     string          `json:"driver"`                                    // Driver implements the label definition configuration
	Definition LabelDefinition `json:"definition"`                                // JSON-encoded definition for the driver
}

type LabelDefinition string

func (r *LabelDefinition) MarshalJSON() ([]byte, error) {
	return []byte(*r), nil
}

func (r *LabelDefinition) UnmarshalJSON(b []byte) error {
	// Store json as string
	*r = LabelDefinition(b)
	return nil
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
