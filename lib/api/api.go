package api

import (
	"github.com/gin-gonic/gin"
)

func InitV1(router *gin.Engine) {
	v1 := router.Group("/api/v1")
	{
		user := v1.Group("/user")
		{
			user.GET("/", UserGetList)
			user.GET("/:id", UserGet)
			user.POST("/:id", UserPost)
			user.DELETE("/:id", UserDelete)
		}
		resource := v1.Group("/resource")
		{
			resource.GET("/", ResourceGetList)
			resource.GET("/:id", ResourceGet)
			resource.POST("/:id", ResourcePost)
			resource.DELETE("/:id", ResourceDelete)
		}
	}
}
