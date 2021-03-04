package fish

import (
	"log"

	"gorm.io/gorm"
)

type App struct {
	db   *gorm.DB
	cfg  *Config
	node *Node
}

func New(db *gorm.DB, cfg_path string, drvs []string) (*App, error) {
	cfg := &Config{}
	if err := cfg.ReadConfigFile(cfg_path); err != nil {
		log.Println("Fish: Unable to apply config file:", cfg_path, err)
		return nil, err
	}

	fish := &App{db: db, cfg: cfg}
	if err := fish.Init(); err != nil {
		return nil, err
	}
	if err := fish.DriversSet(drvs); err != nil {
		return nil, err
	}
	// TODO: provide actual configuration
	if errs := fish.DriversPrepare(cfg.Drivers); errs != nil {
		log.Println("Fish: Unable to prepare some resource drivers", errs)
		if len(drvs) > 0 {
			return nil, errs[0]
		}
	}
	return fish, nil
}

func (e *App) Init() error {
	if err := e.db.AutoMigrate(&User{}, &Node{}, &Label{}, &Resource{}); err != nil {
		log.Println("Fish: Unable to apply DB schema:", err)
		return err
	}

	// Create admin user and ignore errors if it's existing
	_, err := e.UserGet("admin")
	if err == gorm.ErrRecordNotFound {
		if pass, _ := e.UserNew("admin", ""); pass != "" {
			// Print pass of newly created admin user to stderr
			println("Admin user pass:", pass)
		}
	} else if err != nil {
		log.Println("Fish: Unable to create admin due to err:", err)
		return err
	}

	// Init node
	create_node := false
	node, err := e.NodeGet(e.cfg.NodeName)
	if err != nil {
		log.Println("Create new node with name:", e.cfg.NodeName)
		create_node = true
		node = &Node{Name: e.cfg.NodeName}
	} else {
		log.Println("Use existing node with name:", e.cfg.NodeName)
	}

	if err := node.Init(); err != nil {
		log.Println("Fish: Unable to init node due to err:", err)
		return err
	}

	e.node = node
	if create_node {
		if err = e.NodeCreate(e.node); err != nil {
			log.Println("Fish: Unable to create node due to err:", err)
			return err
		}
	} else {
		if err = e.NodeSave(e.node); err != nil {
			log.Println("Fish: Unable to save node due to err:", err)
			return err
		}
	}

	// Run node ping timer
	go e.ping()

	return nil
}

func (e *App) GetNodeID() uint {
	return e.node.ID
}
