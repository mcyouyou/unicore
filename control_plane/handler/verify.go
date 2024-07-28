package handler

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"unicore/control_plane/utils"
	"unicore/model/auth"
)

func VerifyTokenHandler(c *gin.Context) {
	var req auth.VerifyTokenReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Handle token verification logic here

	resp := auth.VerifyTokenResp{Valid: true}
	c.JSON(http.StatusOK, utils.SuccessResp(resp))
}
