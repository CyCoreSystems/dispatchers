package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

var setDefinitions SetDefinitions

// SetDefinition describes a kubernetes dispatcher set's parameters
type SetDefinition struct {
	id        int
	namespace string
	name      string
	port      string
}

// SetDefinitions represents a set of kubernetes dispatcher set parameter definitions
type SetDefinitions struct {
	list []*SetDefinition
}

// String implements flag.Value
func (s *SetDefinitions) String() string {
	var list []string
	for _, d := range s.list {
		list = append(list, d.String())
	}

	return strings.Join(list, ",")
}

// Set implements flag.Value
func (s *SetDefinitions) Set(raw string) error {
	d := new(SetDefinition)

	if err := d.Set(raw); err != nil {
		return err
	}

	s.list = append(s.list, d)
	return nil
}

func (s *SetDefinition) String() string {
	return fmt.Sprintf("%s:%s=%d:%s", s.namespace, s.name, s.id, s.port)
}

// Set configures a kubernetes-derived dispatcher set
func (s *SetDefinition) Set(raw string) (err error) {
	// Handle multiple comma-delimited arguments
	if strings.Contains(raw, ",") {
		args := strings.Split(raw, ",")
		for _, n := range args {
			if err = s.Set(n); err != nil {
				return err
			}
		}
		return nil
	}

	var id int
	ns := "default"
	var name string
	port := "5060"

	if os.Getenv("POD_NAMESPACE") != "" {
		ns = os.Getenv("POD_NAMESPACE")
	}

	pieces := strings.SplitN(raw, "=", 2)
	if len(pieces) < 2 {
		return fmt.Errorf("failed to parse %s as the form [namespace:]name=index", raw)
	}

	naming := strings.SplitN(pieces[0], ":", 2)
	if len(naming) < 2 {
		name = naming[0]
	} else {
		ns = naming[0]
		name = naming[1]
	}

	idString := pieces[1]
	if pieces = strings.Split(pieces[1], ":"); len(pieces) > 1 {
		idString = pieces[0]
		port = pieces[1]
	}

	id, err = strconv.Atoi(idString)
	if err != nil {
		return fmt.Errorf("failed to parse index as an integer: %w", err)
	}

	s.id = id
	s.namespace = ns
	s.name = name
	s.port = port

	return nil
}
