package fish

import (
	"log"
	"math/rand"
	"sync"
	"time"

	"gorm.io/gorm"
)

const ELECTION_ROUND_TIME = 30

type Fish struct {
	db   *gorm.DB
	cfg  *Config
	node *Node

	active_votes_mutex sync.Mutex
	active_votes       []*Vote
	application_mutex  sync.Mutex
	application        *Application
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
		vote.Available = f.application == nil

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

func (f *Fish) executeApplication(app_id int64) error {
	f.application_mutex.Lock()
	{
		if f.application != nil {
			// Seems some application is already executing
			f.application_mutex.Unlock()
			return nil
		}
		f.application, _ = f.ApplicationGet(app_id)
	}
	f.application_mutex.Unlock()

	// Set Application status as ELECTED
	err := f.ApplicationStatusCreate(&ApplicationStatus{
		ApplicationID: f.application.ID,
		Status:        ApplicationStatusElected,
		Description:   "Elected node: " + f.node.Name,
	})
	if err != nil {
		log.Println("Fish: Unable to set application status:", f.application.ID, err)
		return err
	}

	// TODO: Execute application
	log.Println("Fish: Start executing Application", f.application)
	time.Sleep(30 * time.Second)
	log.Println("Fish: Done executing Application", f.application)

	// Set Application status as DEALLOCATED
	err = f.ApplicationStatusCreate(&ApplicationStatus{
		ApplicationID: f.application.ID,
		Status:        ApplicationStatusDeallocated,
		Description:   "Deallocated by: " + f.node.Name,
	})
	if err != nil {
		log.Println("Fish: Unable to set application status:", f.application.ID, err)
		return err
	}

	// Clean current executing application
	f.application_mutex.Lock()
	{
		f.application = nil
	}
	f.application_mutex.Unlock()

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
