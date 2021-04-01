package fish

import (
	"errors"
	"log"
	"strings"
	"time"

	"git.corp.adobe.com/CI/aquarium-fish/lib/crypt"
)

type User struct {
	ID        int64 `gorm:"primaryKey"`
	CreatedAt time.Time
	UpdatedAt time.Time

	Name string     `json:"name" gorm:"unique"`
	Hash crypt.Hash `gorm:"embedded"`
}

func (f *Fish) UserFind(filter string) (us []Label, err error) {
	err = f.db.Where(filter).Find(&us).Error
	return us, err
}

func (f *Fish) UserCreate(u *User) error {
	if u.Name == "" {
		return errors.New("Fish: Name can't be empty")
	}
	if u.Hash.IsEmpty() {
		return errors.New("Fish: Hash can't be empty")
	}

	return f.db.Create(u).Error
}

func (f *Fish) UserSave(u *User) error {
	return f.db.Save(u).Error
}

func (f *Fish) UserGet(name string) (u *User, err error) {
	u = &User{}
	err = f.db.Where("name = ?", name).First(u).Error
	return u, err
}

func (f *Fish) UserAuthBasic(basic string) *User {
	if basic == "" {
		return nil
	}
	split := strings.SplitN(basic, ":", 2)
	return f.UserAuth(split[0], split[len(split)-1])
}

func (f *Fish) UserAuth(name string, password string) *User {
	user, err := f.UserGet(name)
	if err != nil {
		log.Printf("Fish: User not exists: %s", name)
		return nil
	}

	if !user.Hash.IsEqual(password) {
		log.Printf("Fish: Incorrect user password: %s", name)
		return nil
	}

	return user
}

func (f *Fish) UserNew(name string, password string) (string, error) {
	if password == "" {
		password = crypt.RandString(64)
	}

	user := &User{Name: name, Hash: crypt.Generate(password, nil)}

	if err := f.UserCreate(user); err != nil {
		log.Printf("Fish: Unable to create new user: %s, %w", name, err)
		return "", err
	}

	return password, nil
}

func (f *Fish) UserDelete(name string) error {
	return f.db.Where("name = ?", name).Delete(&User{}).Error
}
