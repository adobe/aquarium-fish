package api

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"git.corp.adobe.com/CI/aquarium-fish/lib/fish"
)

type APIv1Processor struct {
	fish *fish.Fish
}

func (e *APIv1Processor) BasicAuth() gin.HandlerFunc {
	realm := "Basic realm=" + strconv.Quote("Authorization Required")
	return func(c *gin.Context) {
		split := strings.SplitN(c.GetHeader("Authorization"), " ", 2)
		data, err := base64.StdEncoding.DecodeString(split[len(split)-1])
		if err != nil {
			// Unable to b64decode creds
			c.Header("WWW-Authenticate", realm)
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		user := e.fish.UserAuthBasic(string(data))
		if user == nil {
			// Credentials doesn't match, we return 401 and abort handlers chain.
			c.Header("WWW-Authenticate", realm)
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		// Clean Auth header and set the user
		c.Request.Header.Del("Authorization")
		c.Set("user", user)
	}
}

func (e *APIv1Processor) UserListGet(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "Get users list"})
}

func (e *APIv1Processor) UserGet(c *gin.Context) {
	//id := c.Param("id")
	c.JSON(http.StatusNotFound, gin.H{"message": "User not found"})
}

func (e *APIv1Processor) UserCreatePost(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "User stored"})
}

func (e *APIv1Processor) UserDelete(c *gin.Context) {
	//id := c.Param("id")
	c.JSON(http.StatusOK, gin.H{"message": "User removed"})
}

func (e *APIv1Processor) ResourceListGet(c *gin.Context) {
	out, err := e.fish.ResourceList()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": fmt.Sprintf("Unable to get the resource list: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Get resource list", "data": out})
}

func (e *APIv1Processor) ResourceGet(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Sprintf("Wrong request param `id`: %v", err)})
		return
	}

	out, err := e.fish.ResourceGet(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": fmt.Sprintf("Resource not found: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Get resource", "data": out})
}

func (e *APIv1Processor) ApplicationListGet(c *gin.Context) {
	out, err := e.fish.ApplicationList()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": fmt.Sprintf("Unable to get the application list: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Get application list", "data": out})
}

func (e *APIv1Processor) ApplicationGet(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Sprintf("Wrong request param `id`: %v", err)})
		return
	}

	out, err := e.fish.ApplicationGet(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": fmt.Sprintf("Application not found: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Get application", "data": out})
}

func (e *APIv1Processor) ApplicationCreatePost(c *gin.Context) {
	var data fish.Application
	if err := c.BindJSON(&data); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Sprintf("Wrong request body: %v", err)})
		return
	}

	// Set the User field out of the authorized user
	user, _ := c.Get("user")
	data.User = user.(*fish.User)

	if err := e.fish.ApplicationCreate(&data); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Sprintf("Unable to create application: %v", err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Application created", "data": data})
}

func (e *APIv1Processor) ApplicationResourceGet(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Sprintf("Wrong request param `id`: %v", err)})
		return
	}

	out, err := e.fish.ResourceGetByApplication(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": fmt.Sprintf("Resource not found: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Get application resource", "data": out})
}

func (e *APIv1Processor) ApplicationStatusGet(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Sprintf("Wrong request param `id`: %v", err)})
		return
	}

	out, err := e.fish.ApplicationStatusGetByApplication(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": fmt.Sprintf("Application status not found: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Get current application status", "data": out})
}

func (e *APIv1Processor) ApplicationDeallocateGet(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Sprintf("Wrong request param `id`: %v", err)})
		return
	}

	out, err := e.fish.ApplicationStatusGetByApplication(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Sprintf("Unable to find status for the application: %s", id)})
		return
	}
	if out.Status != fish.ApplicationStatusAllocated {
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Sprintf("Unable to deallocate the application with status: %s", out.Status)})
		return
	}

	user, _ := c.Get("user")
	as := &fish.ApplicationStatus{ApplicationID: id, Status: fish.ApplicationStatusDeallocate,
		Description: fmt.Sprintf("Requested by user %s", user.(*fish.User).Name),
	}
	err = e.fish.ApplicationStatusCreate(as)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Sprintf("Unable to deallocate the application: %s", id)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Get current application status", "data": as})
}

func (e *APIv1Processor) LabelListGet(c *gin.Context) {
	out, err := e.fish.LabelList()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": fmt.Sprintf("Unable to get the label list: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Get label list", "data": out})
}

func (e *APIv1Processor) LabelGet(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Sprintf("Wrong request param `id`: %v", err)})
		return
	}

	out, err := e.fish.LabelGet(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": fmt.Sprintf("Label not found: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Get label", "data": out})
}

func (e *APIv1Processor) LabelCreatePost(c *gin.Context) {
	var data fish.Label
	if err := c.BindJSON(&data); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Sprintf("Wrong request body: %v", err)})
		return
	}
	if err := e.fish.LabelCreate(&data); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Sprintf("Unable to create label: %v", err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Label created", "data": data})
}

func (e *APIv1Processor) LabelDelete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Sprintf("Wrong request param `id`: %v", err)})
		return
	}
	err = e.fish.LabelDelete(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": fmt.Sprintf("Label delete failed with error: %v", err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Label removed"})
}
