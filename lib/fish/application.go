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

func (f *Fish) ApplicationFind(filter string) (as []Application, err error) {
	err = f.db.Where(filter).Find(&as).Error
	return as, err
}

func (f *Fish) ApplicationCreate(a *Application) error {
	if a.Metadata == "" {
		a.Metadata = "{}"
	}
	err := f.db.Create(a).Error
	// Create ApplicationStatus NEW too
	f.ApplicationStatusCreate(&ApplicationStatus{
		Application: a, Status: ApplicationStatusNew,
		Description: "Just created by Fish " + f.node.Name,
	})
	return err
}

// Intentionally disabled, application can't be updated
/*func (f *Fish) ApplicationSave(app *Application) error {
	return f.db.Save(app).Error
}*/

func (f *Fish) ApplicationGet(id int64) (a *Application, err error) {
	a = &Application{}
	err = f.db.First(a, id).Error
	return a, err
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
