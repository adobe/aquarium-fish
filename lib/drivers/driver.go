package drivers

var DriversList []ResourceDriver

type ResourceDriver interface {
	// Name of the driver
	Name() string

	// Give driver configs and check if it's ok
	Prepare(config []byte) error

	// Allocate the resource with provided labels
	Allocate(labels []string) error

	// Get the status of the resources with given labeles
	Status(labels []string) string

	// Deallocate resource with provided labels
	Deallocate(labels []string) error
}
