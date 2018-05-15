package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joeshaw/carwings"
)

func updateLoop(ctx context.Context, s *carwings.Session) {
	_, err := s.UpdateStatus()
	if err != nil {
		fmt.Printf("Error updating status: %s\n", err)
	}

	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-t.C:
			_, err := s.UpdateStatus()
			if err != nil {
				fmt.Printf("Error updating status: %s\n", err)
			}
		}
	}
}

func runServer(s *carwings.Session, args []string) error {
	var srv http.Server

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-ch
		cancel()
		srv.Shutdown(context.Background())
	}()

	go updateLoop(ctx, s)

	http.HandleFunc("/charging/on", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "POST":
			err := s.ChargingRequest()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

		default:
			http.NotFound(w, r)
			return
		}
	})

	http.HandleFunc("/climate/on", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "POST":
			_, err := s.ClimateOnRequest()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

		default:
			http.NotFound(w, r)
			return
		}
	})

	http.HandleFunc("/climate/off", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "POST":
			_, err := s.ClimateOffRequest()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

		default:
			http.NotFound(w, r)
			return
		}
	})

	srv.Addr = ":8040"
	srv.Handler = nil
	fmt.Printf("Starting HTTP server on %s...\n", srv.Addr)
	return srv.ListenAndServe()
}
