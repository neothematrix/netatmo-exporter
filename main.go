package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/exzz/netatmo-api-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	"github.com/neothematrix/netatmo-exporter/v2/internal/collector"
	"github.com/neothematrix/netatmo-exporter/v2/internal/config"
	"github.com/neothematrix/netatmo-exporter/v2/internal/logger"
	"github.com/neothematrix/netatmo-exporter/v2/internal/token"
	"github.com/neothematrix/netatmo-exporter/v2/internal/web"
	"golang.org/x/oauth2"
)

var (
	signals = []os.Signal{
		syscall.SIGINT,
		syscall.SIGTERM,
	}

	log = logger.NewLogger()
)

func main() {
	cfg, err := config.Parse(os.Args, os.Getenv)
	switch {
	case err == pflag.ErrHelp:
		return
	case err != nil:
		log.Fatalf("Error in configuration: %s", err)
	default:
	}
	log.SetLevel(logrus.Level(cfg.LogLevel))

	client := netatmo.NewClient(cfg.Netatmo)

	if cfg.TokenFile != "" {
		token, err := loadToken(cfg.TokenFile)
		switch {
		case os.IsNotExist(err):
		case err != nil:
			log.Fatalf("Error loading token: %s", err)
		default:
			if token.RefreshToken == "" {
				log.Warn("Restored token has no refresh-token! Exporter will need to be re-authenticated manually.")
			} else if token.Expiry.IsZero() {
				log.Warn("Restored token has no expiry time! Token will be renewed immediately.")
				token.Expiry = time.Now().Add(time.Second)
			}

			log.Infof("Loaded token from %s.", cfg.TokenFile)
			client.InitWithToken(context.Background(), token)
		}

		registerSignalHandler(client, cfg.TokenFile)
	} else {
		log.Warn("No token-file set! Authentication will be lost on restart.")
	}

	metrics := collector.New(log, client.Read, cfg.RefreshInterval, cfg.StaleDuration)
	prometheus.MustRegister(metrics)

	tokenMetric := token.Metric(client.CurrentToken)
	prometheus.MustRegister(tokenMetric)

	if cfg.DebugHandlers {
		http.Handle("/debug/data", web.DebugDataHandler(log, client.Read))
		http.Handle("/debug/token", web.DebugTokenHandler(log, client.CurrentToken))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	http.Handle("/auth/authorize", web.AuthorizeHandler(cfg.ExternalURL, client))
	http.Handle("/auth/callback", web.CallbackHandler(ctx, client))
	http.Handle("/auth/settoken", web.SetTokenHandler(ctx, client))
	http.Handle("/metrics", promhttp.HandlerFor(prometheus.DefaultGatherer, promhttp.HandlerOpts{}))
	http.Handle("/version", versionHandler(log))
	http.Handle("/", web.HomeHandler(client.CurrentToken))

	log.Infof("Listen on %s...", cfg.Addr)
	log.Fatal(http.ListenAndServe(cfg.Addr, nil))
}

func loadToken(fileName string) (*oauth2.Token, error) {
	file, err := os.Open(fileName)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var token oauth2.Token
	if err := json.NewDecoder(file).Decode(&token); err != nil {
		return nil, err
	}

	return &token, nil
}

func registerSignalHandler(client *netatmo.Client, fileName string) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, signals...)
	go func() {
		sig := <-ch
		signal.Reset(signals...)
		log.Debugf("Got signal: %s", sig)

		if err := saveToken(client, fileName); err != nil {
			log.Errorf("Error persisting token: %s", err)
		}

		os.Exit(0)
	}()
}

func saveToken(client *netatmo.Client, fileName string) error {
	token, err := client.CurrentToken()
	switch {
	case err == netatmo.ErrNotAuthenticated:
		return nil
	case err != nil:
		return fmt.Errorf("error retrieving token: %w", err)
	default:
	}

	log.Infof("Saving token to %s ...", fileName)
	data, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("error marshalling token: %w", err)
	}

	if err := os.WriteFile(fileName, data, 0o600); err != nil {
		return fmt.Errorf("error writing token file: %w", err)
	}

	return nil
}
