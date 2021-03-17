package api

import (
	"github.com/gin-gonic/gin"

	"github.com/adobe/aquarium-fish/lib/fish"
)

func InitMetaV1(router *gin.Engine, fish *fish.Fish) {
	proc := &MetaV1Processor{fish: fish}

	v1 := router.Group("/meta/v1")
	v1.Use(
		// Only the local interface which we own can request
		proc.AddressAuth(),
	)
	{
		instance := v1.Group("/data")
		{
			instance.GET("/", proc.DataGetList)
			instance.GET("/:key", proc.DataGet)
		}
	}
}
