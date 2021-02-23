package api

import (
	"net/http"
	"github.com/gin-gonic/gin"

	"github.com/adobe/aquarium-fish/lib/fish"
)

type APIv1Processor struct {
	app *fish.App
}

func (e *APIv1Processor) UserGetList(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "Get users list"})
}

func (e *APIv1Processor) UserGet(c *gin.Context) {
	//id := c.Param("id")
	c.JSON(http.StatusNotFound, gin.H{"message": "User not found"})
}

func (e *APIv1Processor) UserPost(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "User stored"})
}

func (e *APIv1Processor) UserDelete(c *gin.Context) {
	//id := c.Param("id")
	c.JSON(http.StatusOK, gin.H{"message": "User removed"})
}

func (e *APIv1Processor) ResourceGetList(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "Get resources list"})
}

func (e *APIv1Processor) ResourceGet(c *gin.Context) {
	//id := c.Param("id")
	c.JSON(http.StatusNotFound, gin.H{"message": "Resource not found"})
}

func (e *APIv1Processor) ResourcePost(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "Resource stored"})
}

func (e *APIv1Processor) ResourceDelete(c *gin.Context) {
	//id := c.Param("id")
	c.JSON(http.StatusOK, gin.H{"message": "Resource removed"})
}
