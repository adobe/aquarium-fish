package fish

import (
	"log"
	"strings"
	"time"

	"github.com/adobe/aquarium-fish/lib/crypt"
)

type User struct {
	ID        uint `gorm:"primaryKey"`
	CreatedAt time.Time
	UpdatedAt time.Time
	// Unable to use SoftDelete due to error during Save https://gorm.io/docs/delete.html#Soft-Delete

	Name string     `gorm:"unique"`
	Hash crypt.Hash `gorm:"embedded"`
}

func (e *App) UserCreate(user *User) error {
	return e.db.Create(user).Error
}

func (e *App) UserSave(user *User) error {
	return e.db.Save(user).Error
}

func (e *App) UserGet(name string) (user *User, err error) {
	user = &User{}
	err = e.db.Where("name = ?", name).First(user).Error
	return user, err
}

func (e *App) UserAuthBasic(basic string) string {
	if basic == "" {
		return ""
	}
	split := strings.SplitN(basic, ":", 2)
	return e.UserAuth(split[0], split[len(split)-1])
}

func (e *App) UserAuth(name string, password string) string {
	user, err := e.UserGet(name)
	if err != nil {
		log.Printf("Fish: User not exists: %s", name)
		return ""
	}

	if !user.Hash.IsEqual(password) {
		log.Printf("Fish: Incorrect user password: %s", name)
		return ""
	}

	return user.Name
}

func (e *App) UserNew(name string, password string) (string, error) {
	if password == "" {
		password = crypt.RandString(64)
	}

	user := &User{Name: name, Hash: crypt.Generate(password, nil)}

	if err := e.UserCreate(user); err != nil {
		log.Printf("Fish: Unable to create new user: %s, %w", name, err)
		return "", err
	}

	return password, nil
}
