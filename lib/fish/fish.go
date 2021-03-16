package fish

import (
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"gorm.io/gorm"

	"git.corp.adobe.com/CI/aquarium-fish/lib/drivers"
)

const ELECTION_ROUND_TIME = 30

type Fish struct {
	db   *gorm.DB
	cfg  *Config
	node *Node

	active_votes_mutex sync.Mutex
	active_votes       []*Vote
	applications_mutex sync.Mutex
	applications       []int64
}

func New(db *gorm.DB, cfg_path string, drvs []string) (*Fish, error) {
	// Init rand generator
	rand.Seed(time.Now().UnixNano())

	cfg := &Config{}
	if err := cfg.ReadConfigFile(cfg_path); err != nil {
		log.Println("Fish: Unable to apply config file:", cfg_path, err)
		return nil, err
	}

	f := &Fish{db: db, cfg: cfg}
	if err := f.Init(); err != nil {
		return nil, err
	}
	if err := f.DriversSet(drvs); err != nil {
		return nil, err
	}
	// TODO: provide actual configuration
	if errs := f.DriversPrepare(cfg.Drivers); errs != nil {
		log.Println("Fish: Unable to prepare some resource drivers", errs)
		if len(drvs) > 0 {
			return nil, errs[0]
		}
	}
	return f, nil
}

func (f *Fish) Init() error {
	if err := f.db.AutoMigrate(
		&User{},
		&Node{},
		&Label{},
		&Application{},
		&ApplicationStatus{},
		&Resource{},
		&Vote{},
	); err != nil {
		log.Println("Fish: Unable to apply DB schema:", err)
		return err
	}

	// Create admin user and ignore errors if it's existing
	_, err := f.UserGet("admin")
	if err == gorm.ErrRecordNotFound {
		if pass, _ := f.UserNew("admin", ""); pass != "" {
			// Print pass of newly created admin user to stderr
			println("Admin user pass:", pass)
		}
	} else if err != nil {
		log.Println("Fish: Unable to create admin due to err:", err)
		return err
	}

	// Init node
	create_node := false
	node, err := f.NodeGet(f.cfg.NodeName)
	if err != nil {
		log.Println("Create new node with name:", f.cfg.NodeName)
		create_node = true
		node = &Node{Name: f.cfg.NodeName}
	} else {
		log.Println("Use existing node with name:", f.cfg.NodeName)
	}

	if err := node.Init(); err != nil {
		log.Println("Fish: Unable to init node due to err:", err)
		return err
	}

	f.node = node
	if create_node {
		if err = f.NodeCreate(f.node); err != nil {
			log.Println("Fish: Unable to create node due to err:", err)
			return err
		}
	} else {
		if err = f.NodeSave(f.node); err != nil {
			log.Println("Fish: Unable to save node due to err:", err)
			return err
		}
	}

	// Continue to execute the assigned applications
	resources, err := f.ResourceListNode(f.node.ID)
	if err != nil {
		log.Println("Fish: Unable to get the node resources:", err)
		return err
	}
	for _, res := range resources {
		go f.executeApplication(res.ApplicationID)
	}

	// Run node ping timer
	go f.pingProcess()

	// Run application vote process
	go f.checkNewApplicationProcess()

	return nil
}

func (f *Fish) GetNodeID() int64 {
	return f.node.ID
}

func (f *Fish) checkNewApplicationProcess() error {
	check_ticker := time.NewTicker(5 * time.Second)
	for {
		select {
		case <-check_ticker.C:
			new_apps, err := f.ApplicationListGetStatusNew()
			if err != nil {
				log.Println("Fish: Unable to get NEW ApplicationStatus list:", err)
				continue
			}
			for _, app := range new_apps {
				// Check if Vote is already here
				if f.voteActive(app.ID) {
					continue
				}
				log.Println("Fish: NEW Application with no vote:", app)

				// Vote not exists in the active votes - running the process
				f.active_votes_mutex.Lock()
				{
					// Check if it's already exist in the DB (if node was restarted during voting)
					vote, _ := f.VoteGetNodeApplication(f.node.ID, app.ID)

					// Ensure the app & node is set in the vote
					vote.ApplicationID = app.ID
					vote.Node = f.node

					f.active_votes = append(f.active_votes, vote)
					go f.voteProcessRound(vote)
				}
				f.active_votes_mutex.Unlock()
			}
		}
	}
	return nil
}

func (f *Fish) voteProcessRound(vote *Vote) error {
	vote.Round = f.VoteCurrentRoundGet(vote.ApplicationID)

	for {
		start_time := time.Now()
		log.Println("Fish: Starting election round", vote.Round)

		// Determine answer for this round
		vote.Available = f.isNodeAvailableForApplication(vote.ApplicationID)

		// Create vote if it's required
		if vote.NodeID == 0 || vote.ApplicationID == 0 {
			vote.NodeID = vote.Node.ID
			if err := f.VoteCreate(vote); err != nil {
				log.Println("Fish: Unable to create vote:", vote, err)
				return err
			}
		}

		for {
			// Check all the cluster nodes are voted
			nodes, err := f.NodeList()
			if err != nil {
				log.Println("Fish: Unable to get the Node list:", err)
				return err
			}
			votes, err := f.VoteListGetApplicationRound(vote.ApplicationID, vote.Round)
			if err != nil {
				log.Println("Fish: Unable to get the Vote list:", err)
				return err
			}
			if len(votes) == len(nodes) {
				// Ok, all nodes are voted so let's move to election
				// Check if there's yes answers
				available_exists := false
				for _, vote := range votes {
					if vote.Available {
						available_exists = true
						break
					}
				}

				if available_exists {
					// Check if the winner is this node
					vote, err := f.VoteGetElectionWinner(vote.ApplicationID, vote.Round)
					if err != nil {
						log.Println("Fish: Unable to get the election winner:", err)
						return err
					}
					if vote.NodeID == f.node.ID {
						log.Println("Fish: I won the election for Application", vote.ApplicationID)
						go f.executeApplication(vote.ApplicationID)
					} else {
						log.Println("Fish: I lose the election for Application", vote.ApplicationID)
					}
				}

				// Wait till the next round for ELECTION_ROUND_TIME since round start
				t := time.Now()
				to_sleep := start_time.Add(ELECTION_ROUND_TIME * time.Second).Sub(t)
				time.Sleep(to_sleep)

				// Check if the Application changed status
				s, err := f.ApplicationStatusGetByApplication(vote.ApplicationID)
				if err != nil {
					log.Println("Fish: Unable to get the Application status:", err)
					return err
				}
				if s.Status != ApplicationStatusNew {
					// The Application status was changed by some node, so we can drop the election process
					f.voteActiveRemove(vote.ID)
					return nil
				}

				// Next round seems needed
				vote.Round += 1
				break
			}

			log.Println("Fish: Some nodes didn't vote, waiting...")

			// Wait 5 sec and repeat
			time.Sleep(5 * time.Second)
		}
	}
	return nil
}

func (f *Fish) isNodeAvailableForApplication(app_id int64) bool {
	// Is node executing the application right now
	f.applications_mutex.Lock()
	{
		// TODO: Potentially a number of applications could be executed
		// but keep it simple for now
		if len(f.applications) > 0 {
			log.Println("Fish: Node already busy with the application", app_id)
			f.applications_mutex.Unlock()
			return false
		}
	}
	f.applications_mutex.Unlock()

	app, err := f.ApplicationGet(app_id)
	if err != nil {
		log.Println("Fish: Unable to find application", app_id, err)
		return false
	}
	label, err := f.LabelGet(app.LabelID)
	if err != nil {
		log.Println("Fish: Unable to find label", app.LabelID)
		return false
	}

	// Is node supports the required label driver
	drivers := f.DriversGet()
	supports_driver := false
	for _, drv := range drivers {
		if drv.Name() == label.Driver {
			supports_driver = true
			break
		}
	}
	if !supports_driver {
		return false
	}

	return true
}

func (f *Fish) executeApplication(app_id int64) error {
	f.applications_mutex.Lock()
	{
		// TODO: Allow to execute more than one application
		if len(f.applications) > 0 {
			log.Println("Fish: Node already busy with the application", app_id)
			f.applications_mutex.Unlock()
			return nil
		}
		// Check the application is not executed already
		for _, id := range f.applications {
			if id == app_id {
				// Seems the application is already executing
				f.applications_mutex.Unlock()
				return nil
			}
		}
		f.applications = append(f.applications, app_id)
	}
	f.applications_mutex.Unlock()

	app, _ := f.ApplicationGet(app_id)

	// Check current application status
	app_status, err := f.ApplicationStatusGetByApplication(app.ID)
	if err != nil {
		log.Println("Fish: Unable to get the Application status:", err)
		return err
	}

	log.Println("Fish: Start executing Application", app.ID, app_status.Status)

	if app_status.Status == ApplicationStatusNew {
		// Set Application status as ELECTED
		app_status = &ApplicationStatus{ApplicationID: app.ID, Status: ApplicationStatusElected,
			Description: "Elected node: " + f.node.Name,
		}
		err := f.ApplicationStatusCreate(app_status)
		if err != nil {
			log.Println("Fish: Unable to set application status:", app.ID, err)
			return err
		}
	}

	// Get label with the definition
	label, err := f.LabelGet(app.LabelID)
	if err != nil {
		log.Println("Fish: Unable to find label", app.LabelID)
		return err
	}

	// Get or create the new resource object
	res := &Resource{
		Application: app,
		Node:        f.node,
		// TODO: Just copy metadata for now
		Metadata: ResourceMetadata(app.Metadata),
	}
	if app_status.Status == ApplicationStatusAllocated {
		res, err = f.ResourceGetByApplication(app.ID)
		if err != nil {
			log.Println("Fish: Unable to get the allocated resource for Application:", app.ID, err)
			app_status = &ApplicationStatus{ApplicationID: app.ID, Status: ApplicationStatusError,
				Description: fmt.Sprintf("Unable to find the allocated resource: %w", err),
			}
			f.ApplicationStatusCreate(app_status)
		}
	}

	// Locate the required driver
	var driver drivers.ResourceDriver
	drivers := f.DriversGet()
	for i, drv := range drivers {
		if drv.Name() == label.Driver {
			driver = drivers[i]
			break
		}
	}
	if driver == nil {
		log.Println("Fish: Unable to locate driver for the Application", app.ID)
		app_status = &ApplicationStatus{ApplicationID: app.ID, Status: ApplicationStatusError,
			Description: fmt.Sprintf("No driver found"),
		}
		f.ApplicationStatusCreate(app_status)
	}

	// Allocate the resource
	if app_status.Status == ApplicationStatusElected {
		// Run the allocation
		log.Println("Fish: Allocate the resource using the driver", driver.Name())
		res.HwAddr, err = driver.Allocate(string(label.Definition))
		if err != nil {
			log.Println("Fish: Unable to allocate resource for the Application:", app.ID, err)
			app_status = &ApplicationStatus{ApplicationID: app.ID, Status: ApplicationStatusError,
				Description: fmt.Sprintf("Driver allocate resource error: %w", err),
			}
		} else {
			err := f.ResourceCreate(res)
			if err != nil {
				log.Println("Fish: Unable to store resource for Application:", app.ID, err)
			}
			app_status = &ApplicationStatus{ApplicationID: app.ID, Status: ApplicationStatusAllocated,
				Description: fmt.Sprintf("Driver allocated the resource"),
			}
		}
		f.ApplicationStatusCreate(app_status)
	}

	// Run the loop to wait for deallocate request
	for app_status.Status == ApplicationStatusAllocated {
		app_status, err := f.ApplicationStatusGetByApplication(app.ID)
		if err != nil {
			log.Println("Fish: Unable to get status for Application:", app.ID, err)
		}
		if app_status.Status == ApplicationStatusDeallocate {
			// Deallocating and destroy the resource
			err = driver.Deallocate(res.HwAddr)
			if err != nil {
				log.Println("Fish: Unable to get status for Application:", app.ID, err)
				app_status = &ApplicationStatus{ApplicationID: app.ID, Status: ApplicationStatusError,
					Description: fmt.Sprintf("Driver deallocate resource error: %w", err),
				}
			} else {
				err := f.ResourceDelete(res.ID)
				if err != nil {
					log.Println("Fish: Unable to store resource for Application:", app.ID, err)
				}
				app_status = &ApplicationStatus{ApplicationID: app.ID, Status: ApplicationStatusAllocated,
					Description: fmt.Sprintf("Driver allocated the resource"),
				}
			}
			f.ApplicationStatusCreate(app_status)
		} else {
			time.Sleep(5 * time.Second)
		}
	}

	// Clean the executing application
	f.applications_mutex.Lock()
	{
		for i, v := range f.applications {
			if v != app_id {
				continue
			}
			f.applications[i] = f.applications[len(f.applications)-1]
			f.applications = f.applications[:len(f.applications)-1]
			break
		}
	}
	f.applications_mutex.Unlock()

	log.Println("Fish: Done executing Application", app.ID, app_status.Status)

	return nil
}

func (f *Fish) voteActive(app_id int64) bool {
	f.active_votes_mutex.Lock()
	defer f.active_votes_mutex.Unlock()

	for _, vote := range f.active_votes {
		if vote.ApplicationID == app_id {
			return true
		}
	}
	return false
}

func (f *Fish) voteActiveRemove(vote_id int64) {
	f.active_votes_mutex.Lock()
	defer f.active_votes_mutex.Unlock()
	av := f.active_votes

	for i, v := range f.active_votes {
		if v.ID != vote_id {
			continue
		}
		av[i] = av[len(av)-1]
		f.active_votes = av[:len(av)-1]
		break
	}
}