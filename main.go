package main

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/labstack/echo-contrib/echoprometheus"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"html/template"
	"io"
	"net/http"
	"os"
	"os/signal"
	"time"
)

var (
	redisClient *redis.Client
	logger      zerolog.Logger
)

func init() {
	opts, _ := redis.ParseURL(os.Getenv("REDIS_URI"))
	redisClient = redis.NewClient(opts)
}

type Template struct {
	tmpl *template.Template
}

func NewTemplate(parse string) (*Template, error) {
	parsedTmpl, err := template.ParseGlob(parse)
	if err != nil {
		return nil, err
	}

	return &Template{
		tmpl: parsedTmpl,
	}, nil
}

func (t *Template) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	return t.tmpl.ExecuteTemplate(w, name, data)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	logger = zerolog.New(os.Stdout).With().Timestamp().Logger()
	server := echo.New()

	var err error
	server.Renderer, err = NewTemplate("./index.html")
	if err != nil {
		logger.Err(err).Msg("unable to load templates")
	}

	server.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogURI:    true,
		LogStatus: true,
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			logger.Info().
				Str("URI", v.URI).
				Int("status", v.Status).
				Msg("request")

			return nil
		},
	}))

	server.Use(echoprometheus.NewMiddleware("urlshortener"))

	server.GET("/", home)
	server.POST("/shorten", shortenURL)
	server.GET("/s/:id", redirectToURL)

	server.GET("/metrics", echoprometheus.NewHandler())

	go func() {
		if err := server.Start(":" + "APP_PORT"); err != nil {
			logger.Err(err).Msg("failed to start the server")
			os.Exit(1)
		}
	}()

	<-ctx.Done()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Err(err).Msg("failed to gracefully shutdown the server")
		os.Exit(1)
	}
}

func home(c echo.Context) error {
	return c.Render(http.StatusOK, "index.html", nil)
}

func shortenURL(c echo.Context) error {
	url := c.FormValue("url")

	if url == "" {
		return c.String(http.StatusBadRequest, "url cannot be empty")
	}

	short := uuid.New().String()

	shortenUrl := "https://" + c.Request().Host + "/s/" + short

	if os.Getenv("DEV_FLAG") == "1" {
		shortenUrl = "http://" + c.Request().Host + "/s/" + short
	}

	if _, err := redisClient.Set(c.Request().Context(), short, url, 24*time.Hour).Result(); err != nil {
		logger.Err(err).Msg("failed to set shortened url")
		return c.String(http.StatusInternalServerError, "failed to set shortened url")
	}

	return c.String(http.StatusOK, shortenUrl)
}

func redirectToURL(c echo.Context) error {
	shortURL := c.Param("id")

	if shortURL == "" {
		return c.String(http.StatusBadRequest, "short url cannot be empty")
	}

	url, err := redisClient.Get(c.Request().Context(), shortURL).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return c.String(http.StatusNotFound, "url not found")
		}

		logger.Err(err).Msg("failed to get shortened url")

		return c.String(http.StatusInternalServerError, "failed to retrieve url")
	}

	return c.Redirect(http.StatusFound, url)
}
