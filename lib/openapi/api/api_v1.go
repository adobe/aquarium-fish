package api

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
	echomw "github.com/labstack/echo/v4/middleware"

	"github.com/adobe/aquarium-fish/lib/fish"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

// H is a shortcut for map[string]interface{}
type H map[string]interface{}

type Processor struct {
	fish *fish.Fish
}

func NewV1Router(e *echo.Echo, fish *fish.Fish) {
	proc := &Processor{fish: fish}
	router := e.Group("")
	router.Use(
		// Regular basic auth
		echomw.BasicAuth(proc.BasicAuth),
	)
	RegisterHandlers(router, proc)
}

func (e *Processor) BasicAuth(username, password string, c echo.Context) (bool, error) {
	user := e.fish.UserAuth(username, password)

	// Clean Auth header and set the user
	c.Response().Header().Del("Authorization")
	c.Set("user", user)

	// Will pass if user was found
	return user != nil, nil
}

func (e *Processor) UserMeGet(c echo.Context) error {
	user := c.Get("user")
	return c.JSON(http.StatusOK, user)
}

func (e *Processor) UserListGet(c echo.Context, params types.UserListGetParams) error {
	// Only admin can list users
	user := c.Get("user")
	if user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only 'admin' user can list users")})
		return fmt.Errorf("Only 'admin' user can list users")
	}

	out, err := e.fish.UserFind(params.Filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, H{"message": fmt.Sprintf("Unable to get the user list: %v", err)})
		return fmt.Errorf("Unable to get the user list: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

func (e *Processor) UserGet(c echo.Context, id string) error {
	user := c.Get("user")
	if user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only 'admin' user can get user")})
		return fmt.Errorf("Only 'admin' user can get user")
	}

	out, err := e.fish.UserGet(id)
	if err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("User not found: %v", err)})
		return fmt.Errorf("User not found: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

func (e *Processor) UserCreatePost(c echo.Context) error {
	// Only admin can create user
	user := c.Get("user")
	if user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only 'admin' user can create user")})
		return fmt.Errorf("Only 'admin' user can create user")
	}

	var data types.User
	if err := c.Bind(&data); err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Wrong request body: %v", err)})
		return fmt.Errorf("Wrong request body: %w", err)
	}

	password, err := e.fish.UserNew(data.Name, "") // Generate new password for now
	if err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to create user: %v", err)})
		return fmt.Errorf("Unable to create user: %w", err)
	}

	return c.JSON(http.StatusOK, H{"password": password})
}

func (e *Processor) UserDelete(c echo.Context, id string) error {
	// Only admin can delete user
	user := c.Get("user")
	if user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only 'admin' user can delete user")})
		return fmt.Errorf("Only 'admin' user can delete user")
	}

	if err := e.fish.UserDelete(id); err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("User delete failed with error: %v", err)})
		return fmt.Errorf("User delete failed with error: %w", err)
	}

	return c.JSON(http.StatusOK, H{"message": "User removed"})
}

func (e *Processor) ResourceListGet(c echo.Context, params types.ResourceListGetParams) error {
	// Only admin can list the resources
	user := c.Get("user")
	if user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only 'admin' user can list resource")})
		return fmt.Errorf("Only 'admin' user can list resource")
	}

	out, err := e.fish.ResourceFind(params.Filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, H{"message": fmt.Sprintf("Unable to get the resource list: %v", err)})
		return fmt.Errorf("Unable to get the resource list: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

func (e *Processor) ResourceGet(c echo.Context, id int64) error {
	// Only admin can get the resource directly
	user := c.Get("user")
	if user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only 'admin' user can get resource")})
		return fmt.Errorf("Only 'admin' user can get resource")
	}

	out, err := e.fish.ResourceGet(id)
	if err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("Resource not found: %v", err)})
		return fmt.Errorf("Resource not found: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

func (e *Processor) ApplicationListGet(c echo.Context, params types.ApplicationListGetParams) error {
	out, err := e.fish.ApplicationFind(params.Filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, H{"message": fmt.Sprintf("Unable to get the application list: %v", err)})
		return fmt.Errorf("Unable to get the application list: %w", err)
	}

	// Filter the output by owner
	user := c.Get("user")
	if user.(*types.User).Name != "admin" {
		var owner_out []types.Application
		for _, app := range out {
			if app.ID == user.(*types.User).ID {
				owner_out = append(owner_out, app)
			}
		}
		out = owner_out
	}

	return c.JSON(http.StatusOK, out)
}

func (e *Processor) ApplicationGet(c echo.Context, id int64) error {
	out, err := e.fish.ApplicationGet(id)
	if err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("Application not found: %v", err)})
		return fmt.Errorf("Application not found: %w", err)
	}

	// Only the owner of the application (or admin) can request it
	user := c.Get("user")
	if out.ID != user.(*types.User).ID && user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only the owner and admin can request the application")})
		return fmt.Errorf("Only the owner and admin can request the application")
	}

	return c.JSON(http.StatusOK, out)
}

func (e *Processor) ApplicationCreatePost(c echo.Context) error {
	var data types.Application
	if err := c.Bind(&data); err != nil {
		c.JSON(http.StatusBadRequest, H{"error": fmt.Sprintf("Wrong request body: %v", err)})
		return fmt.Errorf("Wrong request body: %w", err)
	}

	// Set the User field out of the authorized user
	user := c.Get("user")
	data.User = user.(*types.User)

	if err := e.fish.ApplicationCreate(&data); err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to create application: %v", err)})
		return fmt.Errorf("Unable to create application: %w", err)
	}

	return c.JSON(http.StatusOK, data)
}

func (e *Processor) ApplicationResourceGet(c echo.Context, id int64) error {
	app, err := e.fish.ApplicationGet(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to find the application: %d", id)})
		return fmt.Errorf("Unable to find the application: %d, %w", id, err)
	}

	// Only the owner of the application (or admin) can request the resource
	user := c.Get("user")
	if app.ID != user.(*types.User).ID && user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only the owner and admin can request the application resource")})
		return fmt.Errorf("Only the owner and admin can request the application resource")
	}

	out, err := e.fish.ResourceGetByApplication(id)
	if err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("Resource not found: %v", err)})
		return fmt.Errorf("Resource not found: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

func (e *Processor) ApplicationStateGet(c echo.Context, id int64) error {
	app, err := e.fish.ApplicationGet(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to find the application: %d", id)})
		return fmt.Errorf("Unable to find the application: %d, %w", id, err)
	}

	// Only the owner of the application (or admin) can request the status
	user := c.Get("user")
	if app.ID != user.(*types.User).ID && user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only the owner and admin can request the application status")})
		return fmt.Errorf("Only the owner and admin can request the application status")
	}

	out, err := e.fish.ApplicationStateGetByApplication(id)
	if err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("Application status not found: %v", err)})
		return fmt.Errorf("Application status not found: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

func (e *Processor) ApplicationDeallocateGet(c echo.Context, id int64) error {
	app, err := e.fish.ApplicationGet(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to find the application: %d", id)})
		return fmt.Errorf("Unable to find the application: %d, %w", id, err)
	}

	// Only the owner of the application (or admin) could deallocate it
	user := c.Get("user")
	if app.ID != user.(*types.User).ID && user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only the owner & admin can deallocate the application resource")})
		return fmt.Errorf("Only the owner & admin can deallocate the application resource")
	}

	out, err := e.fish.ApplicationStateGetByApplication(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to find status for the application: %d", id)})
		return fmt.Errorf("Unable to find status for the application: %d, %w", id, err)
	}
	if out.Status != types.ApplicationStateStatusALLOCATED {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to deallocate the application with status: %s", out.Status)})
		return fmt.Errorf("Unable to deallocate the application with status: %s", out.Status)
	}

	as := &types.ApplicationState{ApplicationID: id, Status: types.ApplicationStateStatusDEALLOCATE,
		Description: fmt.Sprintf("Requested by user %s", user.(*types.User).Name),
	}
	err = e.fish.ApplicationStateCreate(as)
	if err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to deallocate the application: %d", id)})
		return fmt.Errorf("Unable to deallocate the application: %d, %w", id, err)
	}

	return c.JSON(http.StatusOK, as)
}

func (e *Processor) LabelListGet(c echo.Context, params types.LabelListGetParams) error {
	out, err := e.fish.LabelFind(params.Filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, H{"message": fmt.Sprintf("Unable to get the label list: %v", err)})
		return fmt.Errorf("Unable to get the label list: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

func (e *Processor) LabelGet(c echo.Context, id int64) error {
	out, err := e.fish.LabelGet(id)
	if err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("Label not found: %v", err)})
		return fmt.Errorf("Label not found: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

func (e *Processor) LabelCreatePost(c echo.Context) error {
	// Only admin can create label
	user := c.Get("user")
	if user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only 'admin' user can create label")})
		return fmt.Errorf("Only 'admin' user can create label")
	}

	var data types.Label
	if err := c.Bind(&data); err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Wrong request body: %v", err)})
		return fmt.Errorf("Wrong request body: %w", err)
	}
	if err := e.fish.LabelCreate(&data); err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to create label: %v", err)})
		return fmt.Errorf("Unable to create label: %w", err)
	}

	return c.JSON(http.StatusOK, data)
}

func (e *Processor) LabelDelete(c echo.Context, id int64) error {
	// Only admin can delete label
	user := c.Get("user")
	if user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only 'admin' user can delete label")})
		return fmt.Errorf("Only 'admin' user can delete label")
	}

	err := e.fish.LabelDelete(id)
	if err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("Label delete failed with error: %v", err)})
		return fmt.Errorf("Label delete failed with error: %w", err)
	}

	return c.JSON(http.StatusOK, H{"message": "Label removed"})
}

func (e *Processor) VoteListGet(c echo.Context, params types.VoteListGetParams) error {
	user := c.Get("user")
	if user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only 'admin' user can get votes")})
		return fmt.Errorf("Only 'admin' user can get votes")
	}

	out, err := e.fish.VoteFind(params.Filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, H{"message": fmt.Sprintf("Unable to get the vote list: %v", err)})
		return fmt.Errorf("Unable to get the vote list: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

func (e *Processor) LocationListGet(c echo.Context, params types.LocationListGetParams) error {
	user := c.Get("user")
	if user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only 'admin' user can get locations")})
		return fmt.Errorf("Only 'admin' user can get locations")
	}

	out, err := e.fish.LocationFind(params.Filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, H{"message": fmt.Sprintf("Unable to get the location list: %v", err)})
		return fmt.Errorf("Unable to get the location list: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

func (e *Processor) LocationCreatePost(c echo.Context) error {
	user := c.Get("user")
	if user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only 'admin' user can create location")})
		return fmt.Errorf("Only 'admin' user can create location")
	}

	var data types.Location
	if err := c.Bind(&data); err != nil {
		c.JSON(http.StatusBadRequest, H{"error": fmt.Sprintf("Wrong request body: %v", err)})
		return fmt.Errorf("Wrong request body: %w", err)
	}

	if err := e.fish.LocationCreate(&data); err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to create location: %v", err)})
		return fmt.Errorf("Unable to create location: %w", err)
	}

	return c.JSON(http.StatusOK, data)
}

func (e *Processor) ServiceMappingGet(c echo.Context, id int64) error {
	user := c.Get("user")
	if user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only 'admin' user can get service mapping")})
		return fmt.Errorf("Only 'admin' user can get service mapping")
	}

	out, err := e.fish.ServiceMappingGet(id)
	if err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("ServiceMapping not found: %v", err)})
		return fmt.Errorf("ServiceMapping not found: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

func (e *Processor) ServiceMappingListGet(c echo.Context, params types.ServiceMappingListGetParams) error {
	user := c.Get("user")
	if user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only 'admin' user can get service mappings")})
		return fmt.Errorf("Only 'admin' user can get service mappings")
	}

	out, err := e.fish.ServiceMappingFind(params.Filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, H{"message": fmt.Sprintf("Unable to get the servicemappings list: %v", err)})
		return fmt.Errorf("Unable to get the servicemappings list: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

func (e *Processor) ServiceMappingCreatePost(c echo.Context) error {
	var data types.ServiceMapping
	if err := c.Bind(&data); err != nil {
		c.JSON(http.StatusBadRequest, H{"error": fmt.Sprintf("Wrong request body: %v", err)})
		return fmt.Errorf("Wrong request body: %w", err)
	}

	user := c.Get("user")
	if data.ApplicationID != 0 {
		// Only the owner and admin can create servicemapping for his application
		app, err := e.fish.ApplicationGet(data.ApplicationID)
		if err != nil {
			c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to find the application: %d", data.ApplicationID)})
			return fmt.Errorf("Unable to find the application: %d, %w", data.ApplicationID, err)
		}

		if app.ID != user.(*types.User).ID && user.(*types.User).Name != "admin" {
			c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only the owner & admin can assign service mapping to the application")})
			return fmt.Errorf("Only the owner & admin can assign service mapping to the application")
		}
	} else if user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only 'admin' user can create service mapping with undefined application")})
		return fmt.Errorf("Only 'admin' user can create service mapping with undefined application")
	}

	if err := e.fish.ServiceMappingCreate(&data); err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to create service mapping: %v", err)})
		return fmt.Errorf("Unable to create service mapping: %w", err)
	}

	return c.JSON(http.StatusOK, data)
}

func (e *Processor) ServiceMappingDelete(c echo.Context, id int64) error {
	// Only admin can delete ServiceMapping
	user := c.Get("user")
	if user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only 'admin' user can delete service mapping")})
		return fmt.Errorf("Only 'admin' user can delete service mapping")
	}

	if err := e.fish.ServiceMappingDelete(id); err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("ServiceMapping delete failed with error: %v", err)})
		return fmt.Errorf("ServiceMapping delete failed with error: %w", err)
	}

	return c.JSON(http.StatusOK, H{"message": "ServiceMapping removed"})
}
