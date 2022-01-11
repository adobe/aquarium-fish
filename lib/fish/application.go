package fish

import (
	"errors"

	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

func (f *Fish) ApplicationFind(filter *string) (as []types.Application, err error) {
	db := f.db
	if filter != nil {
		db = db.Where(*filter)
	}
	err = db.Find(&as).Error
	return as, err
}

func (f *Fish) ApplicationCreate(a *types.Application) error {
	if a.Metadata == "" {
		a.Metadata = "{}"
	}
	err := f.db.Create(a).Error
	// Create ApplicationState NEW too
	f.ApplicationStateCreate(&types.ApplicationState{
		Application: a, Status: types.ApplicationStateStatusNEW,
		Description: "Just created by Fish " + f.node.Name,
	})
	return err
}

// Intentionally disabled, application can't be updated
/*func (f *Fish) ApplicationSave(app *types.Application) error {
	return f.db.Save(app).Error
}*/

func (f *Fish) ApplicationGet(id int64) (a *types.Application, err error) {
	a = &types.Application{}
	err = f.db.First(a, id).Error
	return a, err
}

func (f *Fish) ApplicationListGetStatusNew() (as []types.Application, err error) {
	// SELECT * FROM applications WHERE ID in (
	//    SELECT application_id FROM (
	//        SELECT application_id, status, max(created_at) FROM application_states GROUP BY application_id
	//    ) WHERE status = "NEW"
	// )
	err = f.db.Where("ID in (?)",
		f.db.Select("application_id").Table("(?)",
			f.db.Model(&types.ApplicationState{}).Select("application_id, status, max(created_at)").Group("application_id"),
		).Where("Status = ?", types.ApplicationStateStatusNEW),
	).Find(&as).Error
	return as, err
}

func (f *Fish) ApplicationIsAllocated(app_id int64) (err error) {
	state, err := f.ApplicationStateGetByApplication(app_id)
	if err != nil {
		return err
	} else if state.Status != types.ApplicationStateStatusALLOCATED {
		return errors.New("Fish: The Application is not allocated")
	}
	return nil
}
