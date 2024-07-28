package handler

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"unicore/control_plane/utils"
	"unicore/model/auth"
)

func RegisterHandler(c *gin.Context) {
	var req auth.RegisterReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Handle register logic here

	resp := auth.RegisterResp{Ok: true}
	c.JSON(http.StatusOK, utils.SuccessResp(resp))
}
