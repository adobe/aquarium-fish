package api

import (
	"net/http"
	"strconv"
	"strings"
	"encoding/base64"
	"github.com/gin-gonic/gin"

	"git.corp.adobe.com/CI/aquarium-fish/lib/fish"
)

type APIv1Processor struct {
	app *fish.App
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
		user := e.app.AuthBasicUser(string(data))
		if user == "" {
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
