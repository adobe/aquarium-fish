package fish

import (
	"math/rand"
	"time"
)

type Vote struct {
	ID        int64 `gorm:"primaryKey"`
	CreatedAt time.Time

	ApplicationID int64        `json:"application_id" gorm:"uniqueIndex:idx_node_app_uniq"`
	Application   *Application `json:"-"` // Application on which node voted

	NodeID int64 `json:"node_id" gorm:"uniqueIndex:idx_node_app_uniq"`
	Node   *Node `json:"-"` // Node voted

	Round     uint16 `json:"round"`     // Round of voting
	Available bool   `json:"available"` // Node can do that - yes or no
	Rand      uint32 `json:"round"`     // Random integer as last resort if the other params are the same
}

func (f *Fish) VoteList() (vs []Vote, err error) {
	err = f.db.Find(&vs).Error
	return vs, err
}

func (f *Fish) VoteCreate(v *Vote) error {
	// Update Vote Rand to be actual rand
	v.Rand = rand.Uint32()
	return f.db.Create(v).Error
}

// Intentionally disabled, vote can't be updated
/*func (f *Fish) VoteSave(v *Vote) error {
	return f.db.Save(v).Error
}*/

func (f *Fish) VoteGet(id int64) (v *Vote, err error) {
	v = &Vote{}
	err = f.db.First(v, id).Error
	return v, err
}

func (f *Fish) VoteCurrentRoundGet(app_id int64) uint16 {
	var result Vote
	f.db.Select("max(round) as round").Where("application_id = ?", app_id).First(&result)
	return result.Round
}

func (f *Fish) VoteListGetApplicationRound(app_id int64, round uint16) (vs []Vote, err error) {
	err = f.db.Where("application_id = ?", app_id).Where("round = ?", round).Find(&vs).Error
	return vs, err
}

func (f *Fish) VoteGetElectionWinner(app_id int64, round uint16) (v *Vote, err error) {
	// Current rule is simple - sort everyone answered "yes" and the first one wins
	v = &Vote{}
	err = f.db.Where("application_id = ?", app_id).Where("round = ?", round).Where("available = ?", true).
		Order("created_at ASC").Order("rand ASC").First(&v).Error
	return v, err
}

func (f *Fish) VoteGetNodeApplication(node_id int64, app_id int64) (v *Vote, err error) {
	// Current rule is simple - sort everyone answered "yes" and the first one wins
	v = &Vote{}
	err = f.db.Where("application_id = ?", app_id).Where("node_id = ?", node_id).Order("round DESC").First(&v).Error
	return v, err
}
