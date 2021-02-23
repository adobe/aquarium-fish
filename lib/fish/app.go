package fish

import (
	"database/sql"
)

type App struct {
	db *sql.DB
}

const (
	schema = "CREATE TABLE IF NOT EXISTS user (id TEXT, password TEXT, UNIQUE(id))"
)


func New(db *sql.DB) (*App, error) {
	fish := &App{ db: db }
	if err := fish.InitDB(); err != nil {
		return nil, err
	}
	return fish, nil
}

func (e *App) InitDB() (error) {
	// TODO: improve schema apply process
	if _, err := e.db.Exec(schema); err != nil {
		return err
	}
	// TODO: init admin user if not exists

	return nil
}

func (e *App) AuthUser(id string, password string) {
	//e.db.Query("SELECT * FROM USER")
}
