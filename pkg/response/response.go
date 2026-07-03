package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type envelope struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Message string      `json:"message,omitempty"`
	Meta    *Meta       `json:"meta,omitempty"`
}

type Meta struct {
	Total  int64 `json:"total"`
	Page   int   `json:"page"`
	Limit  int   `json:"limit"`
}

func OK(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, envelope{Success: true, Data: data})
}

func Created(c *gin.Context, data interface{}) {
	c.JSON(http.StatusCreated, envelope{Success: true, Data: data})
}

func List(c *gin.Context, data interface{}, meta Meta) {
	c.JSON(http.StatusOK, envelope{Success: true, Data: data, Meta: &meta})
}

func Error(c *gin.Context, statusCode int, message string) {
	c.AbortWithStatusJSON(statusCode, envelope{Success: false, Message: message})
}
