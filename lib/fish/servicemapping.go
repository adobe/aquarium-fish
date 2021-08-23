package fish

import (
	"errors"

	"git.corp.adobe.com/CI/aquarium-fish/lib/openapi/types"
)

func (f *Fish) ServiceMappingFind(filter *string) (sms []types.ServiceMapping, err error) {
	db := f.db
	if filter != nil {
		db = db.Where(*filter)
	}
	err = db.Find(&sms).Error
	return sms, err
}

func (f *Fish) ServiceMappingCreate(sm *types.ServiceMapping) error {
	if sm.Service == "" {
		return errors.New("Fish: Service can't be empty")
	}
	if sm.Redirect == "" {
		return errors.New("Fish: Redirect can't be empty")
	}

	return f.db.Create(sm).Error
}

func (f *Fish) ServiceMappingSave(sm *types.ServiceMapping) error {
	return f.db.Save(sm).Error
}

func (f *Fish) ServiceMappingGet(id int64) (sm *types.ServiceMapping, err error) {
	sm = &types.ServiceMapping{}
	err = f.db.First(sm, id).Error
	return sm, err
}

func (f *Fish) ServiceMappingDelete(id int64) error {
	return f.db.Delete(&types.ServiceMapping{}, id).Error
}
