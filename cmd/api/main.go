package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/okakafavour/orderdesk/internal/auth"
	"github.com/okakafavour/orderdesk/internal/config"
	"github.com/okakafavour/orderdesk/internal/database"
	"github.com/okakafavour/orderdesk/internal/handlers"
	"github.com/okakafavour/orderdesk/internal/middleware"
)

func main() {
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		log.Fatalf("load environment file: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load configuration: %v", err)
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	pool, err := database.Connect(ctx, cfg)
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	defer pool.Close()

	log.Println("Connected to PostgreSQL")

	jwtSecret := os.Getenv("JWT_SECRET")
	if len(jwtSecret) < 32 {
		log.Fatal("JWT_SECRET must contain at least 32 characters")
	}

	authService := &auth.Service{
		DB:        pool,
		JWTSecret: []byte(jwtSecret),
	}

	authHandler := &handlers.AuthHandler{
		Service: authService,
	}

	customersHandler := &handlers.CustomersHandler{
		DB: pool,
	}
	productsHandler := &handlers.ProductsHandler{DB: pool}
	ordersHandler := &handlers.OrdersHandler{DB: pool}

	router := gin.Default()

	router.GET("/health", handlers.HealthHandler)

	api := router.Group("/api/v1")
	{
		api.POST("/auth/register", authHandler.Register)
		api.POST("/auth/login", authHandler.Login)
	}

	protected := router.Group("/api/v1")
	protected.Use(middleware.RequireAuth([]byte(jwtSecret)))

	protected.GET("/me", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"user_id":     c.GetString("user_id"),
			"business_id": c.GetString("business_id"),
		})
	})

	protected.POST("/customers", customersHandler.Create)
	protected.GET("/customers", customersHandler.List)
	protected.GET("/customers/:id", customersHandler.Get)
	protected.PUT("/customers/:id", customersHandler.Update)
	protected.DELETE("/customers/:id", customersHandler.Delete)
	protected.POST("/products", productsHandler.Create)
	protected.GET("/products", productsHandler.List)
	protected.GET("/products/:id", productsHandler.Get)
	protected.PUT("/products/:id", productsHandler.Update)
	protected.DELETE("/products/:id", productsHandler.Delete)
	protected.POST("/orders", ordersHandler.Create)
	protected.GET("/orders", ordersHandler.List)
	protected.GET("/orders/:id", ordersHandler.Get)
	protected.PUT("/orders/:id/confirm", ordersHandler.Confirm)
	protected.PUT("/orders/:id/cancel", ordersHandler.Cancel)

	log.Println("OrderDesk API starting on :8080")

	if err := router.Run(":8080"); err != nil {
		log.Fatalf("API server stopped: %v", err)
	}
}
