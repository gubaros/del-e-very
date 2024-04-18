package main

import (
    "github.com/gin-gonic/gin"
    "github.com/gubaros/del-e-very/handlers"
)

func main() {
    router := gin.Default()  // Crea una instancia de Gin
    setupRoutes(router)  // Configura las rutas
    router.Run()  // Inicia el servidor en el puerto por defecto 8080
}

func setupRoutes(router *gin.Engine) {
    router.GET("/ping", func(c *gin.Context) {
        c.JSON(200, gin.H{
            "message": "pong",
        })
    })

    router.POST("/webhooks", handlers.HandleWebhooks)
}

