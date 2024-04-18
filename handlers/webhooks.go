package handlers

import (
    "net/http"
    "github.com/gin-gonic/gin"
    "github.com/gubaros/del-e-very/models" // Asegúrate de importar correctamente tus modelos
)

// HandleWebhooks maneja todos los webhooks entrantes de GitHub
func HandleWebhooks(c *gin.Context) {
    eventType := c.GetHeader("X-GitHub-Event")

    switch eventType {
    case "push":
        handlePushEvent(c)
    case "fork":
        handleForkEvent(c)
    default:
        c.JSON(http.StatusNotImplemented, gin.H{"error": "event not supported"})
    }
}

func handlePushEvent(c *gin.Context) {
    var pushEvent models.PushEvent
    if err := c.BindJSON(&pushEvent); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "invalid data"})
        return
    }
    c.JSON(http.StatusOK, gin.H{"status": "Push event received"})
}

func handleForkEvent(c *gin.Context) {
    var forkEvent models.ForkEvent
    if err := c.BindJSON(&forkEvent); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "invalid data"})
        return
    }
    c.JSON(http.StatusOK, gin.H{"status": "Fork event received"})
}

