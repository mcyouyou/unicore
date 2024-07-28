package main

import (
	"github.com/gin-gonic/gin"
	"log"
	"unicore/control_plane/handler"
)

func main() {
	r := gin.Default()

	r.POST("/register", handler.RegisterHandler)
	r.POST("/login", handler.LoginHandler)
	r.POST("/verify", handler.VerifyTokenHandler)

	if err := r.Run(":32001"); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
