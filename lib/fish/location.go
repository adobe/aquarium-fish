package fish

import (
	"errors"

	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

func (f *Fish) LocationFind(filter *string) (ls []types.Location, err error) {
	db := f.db
	if filter != nil {
		db = db.Where(*filter)
	}
	err = db.Find(&ls).Error
	return ls, err
}

func (f *Fish) LocationCreate(l *types.Location) error {
	if l.Name == "" {
		return errors.New("Fish: Name can't be empty")
	}

	return f.db.Create(l).Error
}

func (f *Fish) LocationSave(l *types.Location) error {
	return f.db.Save(l).Error
}

func (f *Fish) LocationGet(id int64) (l *types.Location, err error) {
	l = &types.Location{}
	err = f.db.First(l, id).Error
	return l, err
}

func (f *Fish) LocationGetByName(name string) (l *types.Location, err error) {
	l = &types.Location{}
	err = f.db.Where("name = ?", name).First(l).Error
	return l, err
}

func (f *Fish) LocationDelete(id int64) error {
	return f.db.Delete(&types.Location{}, id).Error
}
