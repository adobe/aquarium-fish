package fish

import (
	"errors"

	"git.corp.adobe.com/CI/aquarium-fish/lib/openapi/types"
)

func (f *Fish) LabelFind(filter *string) (labels []types.Label, err error) {
	db := f.db
	if filter != nil {
		db = db.Where(*filter)
	}
	err = db.Find(&labels).Error
	return labels, err
}

func (f *Fish) LabelCreate(l *types.Label) error {
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
/*func (f *Fish) LabelSave(label *types.Label) error {
	return f.db.Save(label).Error
}*/

func (f *Fish) LabelGet(id int64) (label *types.Label, err error) {
	label = &types.Label{}
	err = f.db.First(label, id).Error
	return label, err
}

func (f *Fish) LabelDelete(id int64) error {
	return f.db.Delete(&types.Label{}, id).Error
}
