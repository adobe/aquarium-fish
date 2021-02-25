package fish

import (
	"database/sql"
	"log"
)

type App struct {
	db  *sql.DB
	cfg *Config
}

const (
	schema = `CREATE TABLE IF NOT EXISTS user (id TEXT, algo TEXT, salt BLOB, hash BLOB, UNIQUE(id));
			  CREATE TABLE IF NOT EXISTS node (id TEXT, description TEXT, resources TEXT, ping INTEGER, UNIQUE(id));`
)

func New(db *sql.DB, cfg_path string, drvs []string) (*App, error) {
	cfg := &Config{}
	if err := cfg.ReadConfigFile(cfg_path); err != nil {
		log.Println("Unable to apply config file:", cfg_path, err)
		return nil, err
	}

	fish := &App{db: db, cfg: cfg}
	if err := fish.InitDB(); err != nil {
		return nil, err
	}
	if err := fish.DriversSet(drvs); err != nil {
		return nil, err
	}
	// TODO: provide actual configuration
	if errs := fish.DriversPrepare(cfg.Drivers); errs != nil {
		log.Println("Unable to prepare some resource drivers", errs)
		if len(drvs) > 0 {
			return nil, errs[0]
		}
	}
	return fish, nil
}

func (e *App) InitDB() error {
	// TODO: improve schema apply process
	if _, err := e.db.Exec(schema); err != nil {
		log.Println("Unable to apply DB schema:", err)
		return err
	}

	// Create admin user and ignore errors if it's existing
	pass, _ := e.UserNew("admin", "")
	if pass != "" {
		// Print pass to stderr
		println("Admin user pass:", pass)
	}

	return nil
}
