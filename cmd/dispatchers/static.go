package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/CyCoreSystems/dispatchers/v2/sets"
)

var staticSetDefinitions StaticSetDefinitions

// StaticSetDefinition defines a static dispatcher set
type StaticSetDefinition struct {
	id      int
	members []*sets.Endpoint
}

// Set configures a static dispatcher set
func (s *StaticSetDefinition) Set(raw string) (err error) {
	pieces := strings.Split(raw, "=")
	if len(pieces) != 2 {
		return fmt.Errorf("failed to parse static set definition")
	}

	s.id, err = strconv.Atoi(pieces[0])
	if err != nil {
		return fmt.Errorf("failed to parse %s as an integer", pieces[0])
	}

	// Handle multiple comma-delimited arguments
	hostList := strings.Split(pieces[1], ",")
	for _, h := range hostList {
		hostPieces := strings.Split(h, ":")
		switch len(hostPieces) {
		case 1:
			s.members = append(s.members, &sets.Endpoint{
				Address: hostPieces[0],
				Port: 5060,
			})
		case 2:
			port, err := strconv.Atoi(hostPieces[1])
			if err != nil {
				return fmt.Errorf("failed to interpret %s as a port number: %w", hostPieces[1], err)
			}

			s.members = append(s.members, &sets.Endpoint{
				Address: hostPieces[0],
				Port: uint32(port),
			})
		default:
			return fmt.Errorf("failed to parse static set member %s", h)
		}
	}

	return nil
}

func (s *StaticSetDefinition) String() string {
	var membersString []string

	for _, m := range s.Members() {
		membersString = append(membersString, m.String())
	}

	return fmt.Sprintf("%d=%s", s.id, strings.Join(membersString, ","))
}

// Members returns the list of set members, formatted for direct inclusion in the dispatcher set
func (s *StaticSetDefinition) Members() (list []*sets.Endpoint) {
	return append(list, s.members...)
}

// StaticSetDefinitions is a list of static dispatcher sets
type StaticSetDefinitions struct {
	list []*StaticSetDefinition
}

// String implements flag.Value
func (s *StaticSetDefinitions) String() string {
	var list []string
	for _, s := range s.list {
		list = append(list, s.String())
	}
	return strings.Join(list, ",")
}

// Set implements flag.Value
func (s *StaticSetDefinitions) Set(raw string) error {
	d := new(StaticSetDefinition)

	if err := d.Set(raw); err != nil {
		return err
	}

	s.list = append(s.list, d)
	return nil
}
