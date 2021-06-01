package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/CyCoreSystems/dispatchers/v2"
)

type httpService struct {
	c *dispatchers.Controller
}

func (s *httpService) Run(ctx context.Context, addr string) {
	http.HandleFunc("/check/", s.handleIPCheckRequest)
	http.HandleFunc("/dispatcher/", s.handleListSetRequest)
	http.HandleFunc("/dispatchers/", s.handleListSetRequest)

	log.Fatalln(http.ListenAndServe(addr, nil))
}

// Check IP address for membership in a dispatcher set.
// URL:  /check/<setID>/<ip>
func (s *httpService) handleIPCheckRequest(w http.ResponseWriter, r *http.Request) {
	pieces := strings.Split(strings.TrimPrefix(r.URL.Path, "/check/"), "/")
	if len(pieces) != 2 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	setID, err := strconv.Atoi(pieces[0])
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	for _, set := range s.c.CurrentState() {
		if set.ID != setID {
			continue
		}

		for _, ep := range set.Endpoints {
			if ep.Address == pieces[1] {
				w.WriteHeader(http.StatusOK)
				return
			}
		}
	}

	w.WriteHeader(http.StatusNotFound)
}

// Return a given dispatcher set
// URL:  /dispatcher/<setID>
func (s *httpService) handleListSetRequest(w http.ResponseWriter, r *http.Request) {
	pieces := strings.Split(strings.TrimPrefix(r.URL.Path, "/dispatcher/"), "/")
	if len(pieces) != 1 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	setID, err := strconv.Atoi(pieces[0])
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	for _, set := range s.c.CurrentState() {
		if set.ID != setID {
			continue
		}

		w.Header().Add("Content-Type", "application/json")

		if err = json.NewEncoder(w).Encode(set); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
		}
		return
	}

	w.WriteHeader(http.StatusNotFound)
}
