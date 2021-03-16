package drivers

const (
	StatusNone      = "NONE"
	StatusAllocated = "ALLOCATED"
)

var DriversList []ResourceDriver

type ResourceDriver interface {
	// Name of the driver
	Name() string

	// Give driver configs and check if it's ok
	Prepare(config []byte) error

	// Make sure the allocate definition is appropriate
	ValidateDefinition(definition string) error

	// Allocate the resource by definition and returns hw address
	Allocate(definition string) (string, error)

	// Get the status of the resource with given hw address
	Status(hwaddr string) string

	// Deallocate resource with provided hw addr
	Deallocate(hwaddr string) error
}
