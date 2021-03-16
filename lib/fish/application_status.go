package fish

import (
	"errors"
	"time"
)

const (
	ApplicationStatusNew         = "NEW"         // The application just created
	ApplicationStatusElected     = "ELECTED"     // Node is elected during the voting process
	ApplicationStatusAllocated   = "ALLOCATED"   // The resource is allocated and starting up
	ApplicationStatusDeallocate  = "DEALLOCATE"  // User requested the application deallocate
	ApplicationStatusDeallocated = "DEALLOCATED" // The resource is deallocated
	ApplicationStatusError       = "ERROR"       // The error happened
)

type ApplicationStatus struct {
	ID        int64 `gorm:"primaryKey"`
	CreatedAt time.Time

	ApplicationID int64        `json:"application_id"`
	Application   *Application `json:"-"`

	Status      string `json:"status"`      // Value of ApplicationStatus consts
	Description string `json:"description"` // Additional information for the status
}

func (f *Fish) ApplicationStatusList() (ass []ApplicationStatus, err error) {
	err = f.db.Find(&ass).Error
	return ass, err
}

func (f *Fish) ApplicationStatusCreate(as *ApplicationStatus) error {
	if as.Status == "" {
		return errors.New("Fish: Status can't be empty")
	}

	return f.db.Create(as).Error
}

// Intentionally disabled, application status can't be updated
/*func (f *Fish) ApplicationStatusSave(as *ApplicationStatus) error {
	return f.db.Save(as).Error
}*/

func (f *Fish) ApplicationStatusGet(id int64) (as *ApplicationStatus, err error) {
	as = &ApplicationStatus{}
	err = f.db.First(as, id).Error
	return as, err
}

func (f *Fish) ApplicationStatusGetByApplication(app_id int64) (as *ApplicationStatus, err error) {
	as = &ApplicationStatus{}
	err = f.db.Where("application_id = ?", app_id).Order("created_at desc").First(as).Error
	return as, err
}
