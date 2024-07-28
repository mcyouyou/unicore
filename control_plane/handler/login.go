package handler

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"unicore/control_plane/utils"
	"unicore/model/auth"
)

func LoginHandler(c *gin.Context) {
	var req auth.LoginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Handle login logic here

	resp := auth.LoginResp{Succ: true, Token: "example-token"}
	c.JSON(http.StatusOK, utils.SuccessResp(resp))
}
