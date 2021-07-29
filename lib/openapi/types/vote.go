package types

import (
	"time"
)

// TODO: Is not generated right now due to not used in openapi
type Vote struct {
	ID        int64 `gorm:"primaryKey"`
	CreatedAt time.Time

	ApplicationID int64        `json:"application_id" gorm:"uniqueIndex:idx_node_app_round_uniq"`
	Application   *Application `json:"-"` // Application on which node voted

	NodeID int64 `json:"node_id" gorm:"uniqueIndex:idx_node_app_round_uniq"`
	Node   *Node `json:"-"` // Node voted

	Round     uint16 `json:"round" gorm:"uniqueIndex:idx_node_app_round_uniq"` // Round of voting
	Available bool   `json:"available"`                                        // Node can do that - yes or no
	Rand      uint32 `json:"round"`                                            // Random integer as last resort if the other params are the same
}
