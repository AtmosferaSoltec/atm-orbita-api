// Punto de entrada minimo: deja listo el pipeline de despliegue. El resto de la
// aplicacion (modulos, config, base de datos) se construye a partir de aqui.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "healthcheck":
			os.Exit(healthcheck())
		case "migrate":
			// Lo invoca el servicio `migrate` del compose antes de arrancar la app.
			// Sustituir por goose cuando exista la primera migracion.
			slog.Info("no migrations to apply")
			return
		default:
			fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
			os.Exit(2)
		}
	}
	os.Exit(run())
}

func run() int {
	if os.Getenv("APP_ENV") == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	router.Use(gin.Recovery())
	router.GET("/api/v1/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"status": "ok"}})
	})

	server := &http.Server{
		Addr:              ":" + port(),
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server stopped", "error", err)
			os.Exit(1)
		}
	}()
	slog.Info("server started", "addr", server.Addr)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		slog.Error("shutdown failed", "error", err)
		return 1
	}
	return 0
}

// healthcheck lo usa el HEALTHCHECK de Docker: la imagen final no trae curl ni wget.
func healthcheck() int {
	client := http.Client{Timeout: 3 * time.Second}
	res, err := client.Get("http://127.0.0.1:" + port() + "/api/v1/health")
	if err != nil {
		return 1
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func port() string {
	if p := os.Getenv("PORT"); p != "" {
		return p
	}
	return "3001"
}
