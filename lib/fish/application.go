package fish

import (
	"time"

	"github.com/adobe/aquarium-fish/lib/util"
)

type Application struct {
	ID        int64 `gorm:"primaryKey"`
	CreatedAt time.Time

	UserID int64 `json:"user_id"`
	User   *User `json:"-"` // User requested the resource

	LabelID int64  `json:"label_id"`
	Label   *Label `json:"-"` // Label configuration which defines the resource

	Metadata util.UnparsedJson `json:"metadata"` // Requestor metadata in JSON format
}

func (f *Fish) ApplicationList() (apps []Application, err error) {
	err = f.db.Find(&apps).Error
	return apps, err
}

func (f *Fish) ApplicationCreate(app *Application) error {
	err := f.db.Create(app).Error
	// Create ApplicationStatus NEW too
	f.ApplicationStatusCreate(&ApplicationStatus{
		Application: app, Status: ApplicationStatusNew,
		Description: "Just created by Fish " + f.node.Name,
	})
	return err
}

// Intentionally disabled, application can't be updated
/*func (f *Fish) ApplicationSave(app *Application) error {
	return f.db.Save(app).Error
}*/

func (f *Fish) ApplicationGet(id int64) (app *Application, err error) {
	app = &Application{}
	err = f.db.First(app, id).Error
	return app, err
}

func (f *Fish) ApplicationListGetStatusNew() (as []Application, err error) {
	// SELECT * FROM applications WHERE ID in (
	//    SELECT application_id FROM (
	//        SELECT application_id, status, max(created_at) FROM application_statuses GROUP BY application_id
	//    ) WHERE status = "NEW"
	// )
	err = f.db.Where("ID in (?)",
		f.db.Select("application_id").Table("(?)",
			f.db.Model(&ApplicationStatus{}).Select("application_id, status, max(created_at)").Group("application_id"),
		).Where("Status = ?", ApplicationStatusNew),
	).Find(&as).Error
	return as, err
}
