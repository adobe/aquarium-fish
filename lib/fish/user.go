package fish

import (
	"log"
	"strings"

	"github.com/adobe/aquarium-fish/lib/crypt"
)

func (e *App) AuthBasicUser(basic string) string {
	if basic == "" {
		return ""
	}
	split := strings.SplitN(basic, ":", 2)
	return e.AuthUser(split[0], split[len(split)-1])
}

func (e *App) AuthUser(id string, password string) (user_id string) {
	row := e.db.QueryRow("SELECT id, algo, salt, hash FROM user WHERE id = ?", id)

	var hash crypt.Hash
	if err := row.Scan(&user_id, &hash.Algo, &hash.Salt, &hash.Hash); err != nil {
		log.Printf("Unable to parse SQL row data for user: %s, %w", id, err)
		return
	}

	if !hash.IsEqual(password) {
		log.Printf("Incorrect user password: %s", id)
		user_id = ""
	}
	return
}

func (e *App) UserNew(id string, password string) (pass string, err error) {
	if password == "" {
		password = crypt.RandString(64)
	}

	hash := crypt.Generate(password, nil)

	st, err := e.db.Prepare("INSERT INTO user(id, algo, salt, hash) VALUES (?, ?, ?, ?)")
	if err != nil {
		log.Printf("Unable to create new user: %s, %w", id, err)
		return "", err
	}
	_, err = st.Exec(id, hash.Algo, hash.Salt, hash.Hash)
	if err != nil {
		log.Printf("Unable to create new user: %s, %w", id, err)
		return "", err
	}

	return password, nil
}
