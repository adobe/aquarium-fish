package api

import (
	"net/http"
	"github.com/gin-gonic/gin"
)

type User struct {
	Login       string
	Password    string
}
func UserGetList(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "Get users list"})
}

func UserGet(c *gin.Context) {
	//id := c.Param("id")
	c.JSON(http.StatusNotFound, gin.H{"message": "User not found"})
}

func UserPost(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "User stored"})
}

func UserDelete(c *gin.Context) {
	//id := c.Param("id")
	c.JSON(http.StatusOK, gin.H{"message": "User removed"})
}

func ResourceGetList(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "Get resources list"})
}

func ResourceGet(c *gin.Context) {
	//id := c.Param("id")
	c.JSON(http.StatusNotFound, gin.H{"message": "Resource not found"})
}

func ResourcePost(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "Resource stored"})
}

func ResourceDelete(c *gin.Context) {
	//id := c.Param("id")
	c.JSON(http.StatusOK, gin.H{"message": "Resource removed"})
}
