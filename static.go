package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/pkg/errors"
)

var staticSetDefinitions StaticSetDefinitions

// StaticSetMember defines the parameters of a member of a static dispatcher set
type StaticSetMember struct {
	Host string
	Port string
}

func (s *StaticSetMember) String() string {
	return fmt.Sprintf("%s:%s", s.Host, s.Port)
}

// StaticSetDefinition defines a static dispatcher set
type StaticSetDefinition struct {
	id      int
	members []*StaticSetMember
}

// Set configures a static dispatcher set
func (s *StaticSetDefinition) Set(raw string) (err error) {
	pieces := strings.Split(raw, "=")
	if len(pieces) != 2 {
		return errors.New("failed to parse static set definition")
	}

	s.id, err = strconv.Atoi(pieces[0])
	if err != nil {
		return errors.Errorf("failed to parse %s as an integer", pieces[0])
	}

	// Handle multiple comma-delimited arguments
	hostList := strings.Split(pieces[1], ",")
	for _, h := range hostList {
		hostPieces := strings.Split(h, ":")
		switch len(hostPieces) {
		case 1:
			s.members = append(s.members, &StaticSetMember{
				Host: hostPieces[0],
				Port: "5060",
			})
		case 2:
			s.members = append(s.members, &StaticSetMember{
				Host: hostPieces[0],
				Port: hostPieces[1],
			})
		default:
			return errors.Errorf("failed to parse static set member %s", h)
		}
	}

	return nil
}

func (s *StaticSetDefinition) String() string {
	return fmt.Sprintf("%d=%s", s.id, strings.Join(s.Members(), ","))
}

// Members returns the list of set members, formatted for direct inclusion in the dispatcher set
func (s *StaticSetDefinition) Members() (list []string) {
	for _, m := range s.members {
		list = append(list, m.String())
	}
	return
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
