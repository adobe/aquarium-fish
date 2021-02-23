package api

import (
	"github.com/gin-gonic/gin"

	"git.corp.adobe.com/CI/aquarium-fish/lib/fish"
)

func InitV1(router *gin.Engine, fish *fish.App) {
	v1 := router.Group("/api/v1")
	{
		proc := &APIv1Processor{ app: fish }
		user := v1.Group("/user")
		{
			user.GET("/", proc.UserGetList)
			user.GET("/:id", proc.UserGet)
			user.POST("/:id", proc.UserPost)
			user.DELETE("/:id", proc.UserDelete)
		}
		resource := v1.Group("/resource")
		{
			resource.GET("/", proc.ResourceGetList)
			resource.GET("/:id", proc.ResourceGet)
			resource.POST("/:id", proc.ResourcePost)
			resource.DELETE("/:id", proc.ResourceDelete)
		}
	}
}
