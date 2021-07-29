package fish

import (
	"errors"

	"git.corp.adobe.com/CI/aquarium-fish/lib/openapi/types"
)

func (f *Fish) ApplicationStateList() (ass []types.ApplicationState, err error) {
	err = f.db.Find(&ass).Error
	return ass, err
}

func (f *Fish) ApplicationStateCreate(as *types.ApplicationState) error {
	if as.Status == "" {
		return errors.New("Fish: Status can't be empty")
	}

	return f.db.Create(as).Error
}

// Intentionally disabled, application state can't be updated
/*func (f *Fish) ApplicationStateSave(as *types.ApplicationState) error {
	return f.db.Save(as).Error
}*/

func (f *Fish) ApplicationStateGet(id int64) (as *types.ApplicationState, err error) {
	as = &types.ApplicationState{}
	err = f.db.First(as, id).Error
	return as, err
}

func (f *Fish) ApplicationStateGetByApplication(app_id int64) (as *types.ApplicationState, err error) {
	as = &types.ApplicationState{}
	err = f.db.Where("application_id = ?", app_id).Order("created_at desc").First(as).Error
	return as, err
}
