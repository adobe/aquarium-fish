package api

import (
	"github.com/gin-gonic/gin"

	"git.corp.adobe.com/CI/aquarium-fish/lib/fish"
)

func InitV1(router *gin.Engine, fish *fish.Fish) {
	proc := &APIv1Processor{fish: fish}

	v1 := router.Group("/api/v1")
	v1.Use(
		// Regular basic auth
		proc.BasicAuth(),
	)
	{
		me := v1.Group("/me")
		{
			me.GET("/", proc.MeGet)
		}
		user := v1.Group("/user")
		{
			user.GET("/", proc.UserListGet)
			user.GET("/:id", proc.UserGet)
			user.POST("/", proc.UserCreatePost)
			user.DELETE("/:id", proc.UserDelete)
		}
		label := v1.Group("/label")
		{
			label.GET("/", proc.LabelListGet)
			label.GET("/:id", proc.LabelGet)
			label.POST("/", proc.LabelCreatePost)
			label.DELETE("/:id", proc.LabelDelete)
		}
		resource := v1.Group("/resource")
		{
			resource.GET("/", proc.ResourceListGet)
			resource.GET("/:id", proc.ResourceGet)
			// resource.POST - no way to create resource via API, use ResourceRequest instead
			// resource.DELETE - no way to delete resource, only driver can do that
		}
		application := v1.Group("/application")
		{
			application.GET("/", proc.ApplicationListGet)
			application.GET("/:id", proc.ApplicationGet)
			application.POST("/", proc.ApplicationCreatePost)
			// application.DELETE - application is staying here until retention time is reached

			application.GET("/:id/status", proc.ApplicationStatusGet)
			application.GET("/:id/resource", proc.ApplicationResourceGet)
			application.GET("/:id/deallocate", proc.ApplicationDeallocateGet)
		}
	}
}
