package fish

import (
	"errors"
	"log"

	"github.com/adobe/aquarium-fish/lib/crypt"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

func (f *Fish) UserFind(filter *string) (us []types.User, err error) {
	db := f.db
	if filter != nil {
		db = db.Where(*filter)
	}
	err = db.Find(&us).Error
	return us, err
}

func (f *Fish) UserCreate(u *types.User) error {
	if u.Name == "" {
		return errors.New("Fish: Name can't be empty")
	}
	if u.Hash.IsEmpty() {
		return errors.New("Fish: Hash can't be empty")
	}

	return f.db.Create(u).Error
}

func (f *Fish) UserSave(u *types.User) error {
	return f.db.Save(u).Error
}

func (f *Fish) UserGet(name string) (u *types.User, err error) {
	u = &types.User{}
	err = f.db.Where("name = ?", name).First(u).Error
	return u, err
}

func (f *Fish) UserAuth(name string, password string) *types.User {
	// TODO: Make auth process to take constant time in case of failure
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

	user := &types.User{Name: name, Hash: crypt.Generate(password, nil)}

	if err := f.UserCreate(user); err != nil {
		log.Printf("Fish: Unable to create new user: %s, %w", name, err)
		return "", err
	}

	return password, nil
}

func (f *Fish) UserDelete(name string) error {
	return f.db.Where("name = ?", name).Delete(&types.User{}).Error
}
